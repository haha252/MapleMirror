package indexnow

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestV1StateIsLegacyEvenWhenRevisionLooksCurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "indexnow.state")
	if err := os.WriteFile(path, []byte(`{"key":"key","revision":"current"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	state, legacy, err := loadState(path)
	if err != nil || !legacy || state.Revision != "current" {
		t.Fatalf("v1 state=%+v legacy=%v err=%v", state, legacy, err)
	}
}

func TestV1ManagerForcesFullSnapshotBootstrap(t *testing.T) {
	directory := t.TempDir()
	key := strings.Repeat("a", 64)
	keyPath := filepath.Join(directory, "key")
	statePath := filepath.Join(directory, "state")
	if err := os.WriteFile(keyPath, []byte(key), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte(`{"key":"`+key+`","revision":"current"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: keyPath, StateFile: statePath, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Snapshot{}
	for _, path := range []string{"/", "/about", "/api-docs", "/stats", "/changelog", "/p1/"} {
		snapshot[path] = Page{Path: path, Fingerprint: "sha256:" + path, Present: true}
	}
	if queued := manager.ReconcileSnapshot(context.Background(), snapshot); queued != len(snapshot) {
		t.Fatalf("v1 bootstrap queued=%d want %d", queued, len(snapshot))
	}
	manager.flushPending()
	payload := waitPayload(t, requests)
	manager.Close()
	if len(payload.URLList) != len(snapshot) {
		t.Fatalf("v1 bootstrap payload=%d want %d", len(payload.URLList), len(snapshot))
	}
	state, legacy, err := loadState(statePath)
	if err != nil || legacy || state.Version != stateVersion || len(state.Pages) != len(snapshot) {
		t.Fatalf("migrated state=%+v legacy=%v err=%v", state, legacy, err)
	}
}

func TestV2StateRoundTripsPagesAndErrorsAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "indexnow.state")
	state := emptyState("key")
	state.Pages["/"] = pageState{Fingerprint: "sha256:home", SubmittedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	state.Errors["/broken/"] = errorState{Fingerprint: "sha256:bad", Kind: "permanent",
		Message: "HTTP 400", RecordedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	if err := writeState(path, state); err != nil {
		t.Fatal(err)
	}
	loaded, legacy, err := loadState(path)
	if err != nil || legacy || loaded.Version != stateVersion || loaded.Pages["/"].Fingerprint != "sha256:home" ||
		loaded.Errors["/broken/"].Kind != "permanent" {
		t.Fatalf("v2 state=%+v legacy=%v err=%v", loaded, legacy, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state permissions=%o want 600", info.Mode().Perm())
	}
	var raw map[string]any
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["version"] != float64(2) {
		t.Fatalf("state version=%v want 2", raw["version"])
	}
}

func TestSuccessfulSubmissionStoresFingerprintAndRestartSkipsIt(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: filepath.Join(directory, "state"),
		HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.NotifyChanges(context.Background(), []PageChange{{Path: "/about", Fingerprint: "sha256:about", Present: true}})
	manager.flushPending()
	waitPayload(t, requests)
	manager.Close()

	state, legacy, err := loadState(filepath.Join(directory, "state"))
	if err != nil || legacy || state.Pages["/about"].Fingerprint != "sha256:about" {
		t.Fatalf("stored state=%+v legacy=%v err=%v", state, legacy, err)
	}
	secondTransport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: make(chan requestPayload, 1)}
	second, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: filepath.Join(directory, "state"),
		HTTPClient: &http.Client{Transport: secondTransport}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if queued := second.ReconcileSnapshot(context.Background(), Snapshot{
		"/about": {Path: "/about", Fingerprint: "sha256:about", Present: true},
	}); queued != 0 {
		t.Fatalf("unchanged restart queued=%d want 0", queued)
	}
}

func TestFailedSubmissionDoesNotStoreSuccessFingerprint(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan requestPayload, maxAttempts)
	transport := &recordingTransport{statuses: []int{500, 500, 500, 500}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: filepath.Join(directory, "state"),
		HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.NotifyChanges(context.Background(), []PageChange{{Path: "/about", Fingerprint: "sha256:about", Present: true}})
	manager.flushPending()
	for i := 0; i < maxAttempts; i++ {
		waitPayload(t, requests)
	}
	manager.Close()
	state, _, err := loadState(filepath.Join(directory, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Pages["/about"]; ok {
		t.Fatalf("failed page was marked successful: %+v", state)
	}
}

func TestStateWriteFailureRequeuesSuccessfulHTTPBatch(t *testing.T) {
	directory := t.TempDir()
	statePath := filepath.Join(directory, "state-dir")
	if err := os.Mkdir(statePath, 0o700); err != nil {
		t.Fatal(err)
	}
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: statePath,
		HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.NotifyChanges(context.Background(), []PageChange{{Path: "/about", Fingerprint: "sha256:about", Present: true}})
	manager.flushPending()
	waitPayload(t, requests)
	if !manager.hasPending() {
		t.Fatal("state write failure dropped the batch instead of requeueing it")
	}
	manager.Close()
}

func TestPendingQueueRetainsLatestFingerprint(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: filepath.Join(directory, "state"),
		HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.NotifyChanges(context.Background(), []PageChange{{Path: "/about", Fingerprint: "sha256:old", Present: true}})
	manager.NotifyChanges(context.Background(), []PageChange{{Path: "/about", Fingerprint: "sha256:new", Present: true}})
	manager.mu.Lock()
	got := manager.pending["/about"].Fingerprint
	manager.mu.Unlock()
	if got != "sha256:new" {
		t.Fatalf("pending fingerprint=%q want latest", got)
	}
	manager.flushPending()
	waitPayload(t, requests)
	manager.Close()
	state, _, err := loadState(filepath.Join(directory, "state"))
	if err != nil || state.Pages["/about"].Fingerprint != "sha256:new" {
		t.Fatalf("latest fingerprint was not submitted: %+v err=%v", state, err)
	}
}

func TestPermanentFailureIsSuppressedUntilManualForce(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan requestPayload, 2)
	transport := &recordingTransport{statuses: []int{http.StatusBadRequest, http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(directory, "key"), StateFile: filepath.Join(directory, "state"),
		HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	change := PageChange{Path: "/about", Fingerprint: "sha256:about", Present: true}
	manager.NotifyChanges(context.Background(), []PageChange{change})
	manager.flushPending()
	waitPayload(t, requests)
	manager.NotifyChanges(context.Background(), []PageChange{change})
	if manager.hasPending() {
		t.Fatal("same permanent failure should be suppressed")
	}
	if queued := manager.NotifySnapshotNow(context.Background(), Snapshot{"/about": change}); queued != 1 {
		t.Fatalf("manual force queued=%d want 1", queued)
	}
	manager.flushPending()
	waitPayload(t, requests)
	manager.Close()
	if transport.attempts != 2 {
		t.Fatalf("manual force attempts=%d want 2", transport.attempts)
	}
	state, _, err := loadState(filepath.Join(directory, "state"))
	if err != nil || state.Pages["/about"].Fingerprint != change.Fingerprint {
		t.Fatalf("manual retry did not store success: %+v err=%v", state, err)
	}
}
