package mirrorsync

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPGitHubClientTimesOutStalledRequest(t *testing.T) {
	client := HTTPGitHubClient{
		Client:  &http.Client{Transport: blockingTransport{}},
		Timeout: 20 * time.Millisecond,
	}
	started := time.Now()
	_, err := client.ListReleases(context.Background(), "owner/repo")
	if err == nil {
		t.Fatal("stalled GitHub request should fail")
	}
	if time.Since(started) > time.Second {
		t.Fatal("stalled GitHub request did not return before test deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) &&
		!strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("expected deadline error, got %v", err)
	}
}

type blockingTransport struct{}

func (blockingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	<-req.Context().Done()
	return nil, req.Context().Err()
}
