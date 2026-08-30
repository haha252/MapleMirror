package developerapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"mirror-server/internal/config"
)

type fakeProjectSource struct {
	projects config.Projects
}

func (f fakeProjectSource) Load() (config.Projects, error) { return f.projects, nil }
func (f fakeProjectSource) Current() config.Projects       { return f.projects }

type fakeSyncTrigger struct {
	mu      sync.Mutex
	calls   []string
	started chan string
	block   <-chan struct{}
}

func (f *fakeSyncTrigger) Trigger(_ context.Context, projectID, _ string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, projectID)
	f.mu.Unlock()
	if f.started != nil {
		f.started <- projectID
	}
	if f.block != nil {
		<-f.block
	}
	return "scan-test", nil
}

func developerRequest(method, projectID, token string) *http.Request {
	req := httptest.NewRequest(method,
		"/api/developer/v1/projects/"+projectID+"/sync", nil)
	req.RemoteAddr = "192.0.2.10:54321"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func TestHandlerAcceptsBoundProjectAndRejectsCrossProjectUse(t *testing.T) {
	store, _ := testStore(t)
	result, err := store.RotateToken(context.Background(), "p1")
	if err != nil {
		t.Fatal(err)
	}
	syncer := &fakeSyncTrigger{started: make(chan string, 1)}
	handler := NewHandler(store, fakeProjectSource{config.Projects{Projects: []config.Project{
		{ID: "p1", Enabled: true}, {ID: "p2", Enabled: true},
	}}}, syncer, nil, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, developerRequest(http.MethodPost, "p1", result.Token))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("accepted request status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-RateLimit-Limit") != "100" ||
		rec.Header().Get("X-RateLimit-Remaining") != "99" {
		t.Fatalf("unexpected rate headers: %+v", rec.Header())
	}
	select {
	case projectID := <-syncer.started:
		if projectID != "p1" {
			t.Fatalf("triggered project=%q", projectID)
		}
	case <-time.After(time.Second):
		t.Fatal("sync trigger did not run")
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, developerRequest(http.MethodPost, "p2", result.Token))
	if rec.Code != http.StatusForbidden ||
		!strings.Contains(rec.Body.String(), "TOKEN_PROJECT_MISMATCH") {
		t.Fatalf("cross-project status=%d body=%s", rec.Code, rec.Body.String())
	}
	usage, err := store.Usage(context.Background(), "p1", time.Now())
	if err != nil || usage.Used != 1 {
		t.Fatalf("cross-project call must not consume quota: usage=%+v err=%v", usage, err)
	}
}

func TestHandlerCoalescesConcurrentProjectScanWithoutConsumingQuota(t *testing.T) {
	store, _ := testStore(t)
	result, _ := store.RotateToken(context.Background(), "p1")
	block := make(chan struct{})
	syncer := &fakeSyncTrigger{started: make(chan string, 1), block: block}
	handler := NewHandler(store, fakeProjectSource{config.Projects{Projects: []config.Project{
		{ID: "p1", Enabled: true},
	}}}, syncer, nil, nil)

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, developerRequest(http.MethodPost, "p1", result.Token))
	if first.Code != http.StatusAccepted {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	select {
	case <-syncer.started:
	case <-time.After(time.Second):
		t.Fatal("first trigger did not start")
	}

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, developerRequest(http.MethodPost, "p1", result.Token))
	if second.Code != http.StatusAccepted {
		t.Fatalf("second status=%d body=%s", second.Code, second.Body.String())
	}
	var body struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data["already_running"] != true {
		t.Fatalf("second response did not report already_running: %s", second.Body.String())
	}
	usage, err := store.Usage(context.Background(), "p1", time.Now())
	if err != nil || usage.Used != 1 {
		t.Fatalf("coalesced request consumed quota: usage=%+v err=%v", usage, err)
	}
	close(block)
}

func TestHandlerRejectsDisabledProjectAndEnforcesDailyLimit(t *testing.T) {
	store, _ := testStore(t)
	store.DailyLimit = 1
	result, _ := store.RotateToken(context.Background(), "p1")
	projects := fakeProjectSource{config.Projects{Projects: []config.Project{{ID: "p1", Enabled: false}}}}
	handler := NewHandler(store, projects, &fakeSyncTrigger{}, nil, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, developerRequest(http.MethodPost, "p1", result.Token))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "PROJECT_DISABLED") {
		t.Fatalf("disabled status=%d body=%s", rec.Code, rec.Body.String())
	}

	projects.projects.Projects[0].Enabled = true
	handler.Projects = projects
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, developerRequest(http.MethodPost, "p1", result.Token))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("first enabled status=%d body=%s", rec.Code, rec.Body.String())
	}
	for i := 0; i < 50 && handler.isInflight("p1"); i++ {
		time.Sleep(time.Millisecond)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, developerRequest(http.MethodPost, "p1", result.Token))
	if rec.Code != http.StatusTooManyRequests ||
		!strings.Contains(rec.Body.String(), "RATE_LIMIT_EXCEEDED") {
		t.Fatalf("limit status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-RateLimit-Remaining") != "0" ||
		rec.Header().Get("Retry-After") == "" {
		t.Fatalf("missing limit headers: %+v", rec.Header())
	}
}

func TestHandlerRequiresBearerToken(t *testing.T) {
	store, _ := testStore(t)
	handler := NewHandler(store, fakeProjectSource{config.Projects{Projects: []config.Project{
		{ID: "p1", Enabled: true},
	}}}, &fakeSyncTrigger{}, nil, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, developerRequest(http.MethodPost, "p1", ""))
	if rec.Code != http.StatusUnauthorized ||
		rec.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatalf("unauthorized status=%d headers=%v body=%s",
			rec.Code, rec.Header(), rec.Body.String())
	}
}
