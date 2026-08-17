package indexnow

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyChangesUsesFiveMinuteProductionDefault(t *testing.T) {
	if (Options{}).Debounce != 0 {
		t.Fatal("zero options should allow the manager to apply its default debounce")
	}
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(t.TempDir(), "key"), HTTPClient: &http.Client{Transport: transport}, Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.NotifyChanges(context.Background(), []PageChange{{Path: "/about", Fingerprint: "sha256:about", Present: true}})
	select {
	case <-requests:
		t.Fatal("debounced submission happened too early")
	case <-time.After(5 * time.Millisecond):
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("debounced submission did not happen")
	}
}

func TestManualSnapshotSubmissionBypassesDebounce(t *testing.T) {
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(t.TempDir(), "key"), HTTPClient: &http.Client{Transport: transport}, Debounce: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	queued := manager.NotifySnapshotNow(context.Background(), Snapshot{
		"/": {Path: "/", Fingerprint: "sha256:home", Present: true},
	})
	if queued != 1 {
		t.Fatalf("manual queued=%d want 1", queued)
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("manual submission waited for debounce")
	}
}
