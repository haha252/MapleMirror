package adminui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/master/developerapi"
	"mirror-server/internal/master/mirrorsync"
)

func TestProjectDeveloperAPITokenLifecycle(t *testing.T) {
	server, db := newTestServer(t)
	server.developerAPI = developerapi.NewStore(db, time.UTC, "https://mirror.example")
	server.projects = mirrorsync.NewProjectLoader("", config.Projects{Projects: []config.Project{
		{ID: "p1", Name: "Project One", Repository: "owner/repo", Enabled: true},
	}})

	infoRec := httptest.NewRecorder()
	server.projectActionAPI(infoRec, httptest.NewRequest(http.MethodGet,
		"/admin/api/projects/p1/developer-api", nil))
	var initial developerapi.APIInfo
	if err := json.Unmarshal(infoRec.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if infoRec.Code != http.StatusOK || initial.TokenConfigured ||
		initial.Endpoint != "https://mirror.example/api/developer/v1/projects/p1/sync" {
		t.Fatalf("initial info status=%d info=%+v", infoRec.Code, initial)
	}

	firstToken, firstInfo := rotateDeveloperTokenRequest(t, server, "p1")
	if !strings.HasPrefix(firstToken, "mmdev_") {
		t.Fatalf("unexpected token %q", firstToken)
	}
	if firstInfo.TokenPrefix == "" || !firstInfo.TokenConfigured {
		t.Fatalf("unexpected developer api info: %+v", firstInfo)
	}

	infoRec = httptest.NewRecorder()
	server.projectActionAPI(infoRec, httptest.NewRequest(http.MethodGet,
		"/admin/api/projects/p1/developer-api", nil))
	if strings.Contains(infoRec.Body.String(), firstToken) {
		t.Fatal("full token must not be returned by info endpoint")
	}
	if !strings.Contains(infoRec.Body.String(), firstInfo.TokenPrefix) {
		t.Fatalf("info endpoint should return token prefix: %s", infoRec.Body.String())
	}

	secondToken, _ := rotateDeveloperTokenRequest(t, server, "p1")
	if secondToken == firstToken {
		t.Fatal("reset token must be different")
	}
	if _, _, err := server.developerAPI.Authenticate(context.Background(), firstToken); !errors.Is(err, developerapi.ErrUnauthorized) {
		t.Fatalf("old token remains valid after reset: %v", err)
	}
	if projectID, _, err := server.developerAPI.Authenticate(context.Background(), secondToken); err != nil || projectID != "p1" {
		t.Fatalf("new token invalid: project=%q err=%v", projectID, err)
	}

	var audits int
	query := "SELECT COUNT(*) FROM admin_audit_events WHERE operation = 'project.developer_token.rotate' AND target_id = 'p1'"
	if err := db.QueryRow(query).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("token rotations audit count=%d want 2", audits)
	}
}

func TestProjectsStaticIncludesDeveloperAPIControls(t *testing.T) {
	server, _ := newTestServer(t)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/admin/projects.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("projects.js status=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "adminProjectDeveloper") {
		t.Fatal("projects.js must initialize the developer API controls")
	}
	helper := httptest.NewRecorder()
	server.Handler().ServeHTTP(helper, httptest.NewRequest(http.MethodGet, "/static/admin/project-developer.js", nil))
	if helper.Code != http.StatusOK {
		t.Fatalf("project-developer.js status=%d", helper.Code)
	}
	body += helper.Body.String()
	for _, want := range []string{
		"开发者 API", "Developer Sync API", "/developer-api/token",
		"重置 Token", "今日使用", "Authorization: Bearer",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("projects.js missing %q", want)
		}
	}
}

func rotateDeveloperTokenRequest(t *testing.T, server *Server,
	projectID string) (string, developerapi.APIInfo) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/projects/"+projectID+"/developer-api/token",
		strings.NewReader("{}"))
	req = withAdminUser(req)
	rec := httptest.NewRecorder()
	server.projectActionAPI(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate token status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("token response cache-control=%q", rec.Header().Get("Cache-Control"))
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	var token string
	var info developerapi.APIInfo
	if err := json.Unmarshal(raw["token"], &token); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw["developer_api"], &info); err != nil {
		t.Fatal(err)
	}
	return token, info
}
