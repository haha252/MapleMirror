package indexnow

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBootstrapPayloadUsesApexHostAndPublicPages(t *testing.T) {
	requests := make(chan requestPayload, 2)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "indexnow.key")
	statePath := filepath.Join(dir, "indexnow.state")
	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: keyPath, StateFile: statePath, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.Bootstrap([]string{"p1", "p2"}, "revision-1")
	payload := waitPayload(t, requests)
	manager.Close()

	if payload.Host != "fyhub.cn" {
		t.Fatalf("IndexNow host=%q, want fyhub.cn", payload.Host)
	}
	if payload.Key != manager.Key() || payload.KeyLocation != "https://fyhub.cn/"+manager.Key()+".txt" {
		t.Fatalf("unexpected key fields: %+v", payload)
	}
	want := map[string]bool{
		"https://fyhub.cn/": true, "https://fyhub.cn/about": true,
		"https://fyhub.cn/api-docs": true, "https://fyhub.cn/stats": true,
		"https://fyhub.cn/changelog": true,
		"https://fyhub.cn/p1/":       true, "https://fyhub.cn/p2/": true,
	}
	got := make(map[string]bool, len(payload.URLList))
	for _, value := range payload.URLList {
		got[value] = true
	}
	if len(got) != len(want) {
		t.Fatalf("bootstrap URL count=%d want %d: %v", len(got), len(want), got)
	}
	for value := range want {
		if !got[value] {
			t.Fatalf("bootstrap payload missing %q: %v", value, got)
		}
	}

	manager, err = New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: keyPath, StateFile: statePath, HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.Bootstrap([]string{"p1", "p2"}, "revision-1")
	defer manager.Close()
	select {
	case duplicate := <-requests:
		t.Fatalf("same bootstrap revision was submitted again: %+v", duplicate)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestNotifyPathsDeduplicatesAndRetriesTransientErrors(t *testing.T) {
	requests := make(chan requestPayload, 4)
	transport := &recordingTransport{statuses: []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusOK}, payloads: requests}

	manager, err := New(Options{BaseURL: "https://fyhub.cn", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(t.TempDir(), "key"), HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.NotifyPaths(context.Background(), []string{"/about", "/about", "/stats"})
	var last requestPayload
	for i := 0; i < 3; i++ {
		last = waitPayload(t, requests)
	}
	manager.Close()
	if transport.attempts != 3 {
		t.Fatalf("transient failures attempts=%d want 3", transport.attempts)
	}
	got := make(map[string]bool, len(last.URLList))
	for _, value := range last.URLList {
		got[value] = true
	}
	if len(got) != 2 || !got["https://fyhub.cn/about"] || !got["https://fyhub.cn/stats"] {
		t.Fatalf("deduplicated URL list=%v", got)
	}
}

func TestConfiguredBaseURLControlsIndexNowHost(t *testing.T) {
	requests := make(chan requestPayload, 1)
	transport := &recordingTransport{statuses: []int{http.StatusAccepted}, payloads: requests}
	manager, err := New(Options{BaseURL: "https://mirror.example.com", Endpoint: "https://api.indexnow.test/indexnow",
		KeyFile: filepath.Join(t.TempDir(), "key"), HTTPClient: &http.Client{Transport: transport}})
	if err != nil {
		t.Fatal(err)
	}
	manager.NotifyPaths(context.Background(), []string{"/about"})
	payload := waitPayload(t, requests)
	manager.Close()
	if payload.Host != "mirror.example.com" || payload.KeyLocation != "https://mirror.example.com/"+manager.Key()+".txt" {
		t.Fatalf("configured base URL was not used: %+v", payload)
	}
}

func waitPayload(t *testing.T, requests <-chan requestPayload) requestPayload {
	t.Helper()
	select {
	case payload := <-requests:
		return payload
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for IndexNow request")
		return requestPayload{}
	}
}

type recordingTransport struct {
	statuses       []int
	responseBodies []string
	roundTripErr   error
	payloads       chan<- requestPayload
	attempts       int
}

func (t *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.attempts++
	var payload requestPayload
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	t.payloads <- payload
	status := http.StatusOK
	if t.attempts <= len(t.statuses) {
		status = t.statuses[t.attempts-1]
	}
	responseBody := ""
	if t.attempts <= len(t.responseBodies) {
		responseBody = t.responseBodies[t.attempts-1]
	}
	if t.roundTripErr != nil {
		return nil, t.roundTripErr
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(responseBody)), Header: make(http.Header), Request: request}, nil
}
