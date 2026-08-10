package mirrorsync

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"mirror-server/internal/config"
)

type recordingLimitedGitHubClient struct {
	limit int
}

func (c *recordingLimitedGitHubClient) ListReleases(context.Context, string) ([]GitHubRelease, error) {
	return nil, errors.New("unlimited method must not be called")
}

func (c *recordingLimitedGitHubClient) ListReleasesLimited(_ context.Context, _ string,
	limit int) ([]GitHubRelease, error) {
	c.limit = limit
	return []GitHubRelease{}, nil
}

func TestGitHubReleaseSourcePassesRetainVersionsAsRecoveryLimit(t *testing.T) {
	client := &recordingLimitedGitHubClient{}
	source := GitHubReleaseSource{Client: client}
	_, err := source.ListResourceVersions(context.Background(), config.Project{
		Repository: "owner/repo", RetainVersions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.limit != 3 {
		t.Fatalf("recovery limit got=%d want=3", client.limit)
	}
}

func TestHTTPGitHubClientLimitsAtomRecovery(t *testing.T) {
	hydrated := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path + "?" + req.URL.RawQuery
		switch key {
		case "/repos/owner/repo/releases?per_page=100":
			return &http.Response{StatusCode: http.StatusBadGateway,
				Status: "502 Bad Gateway", Body: io.NopCloser(strings.NewReader("bad gateway"))}, nil
		case "/owner/repo/releases.atom?":
			return githubAtomResponse(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">
				<entry><link href="https://github.com/owner/repo/releases/tag/v3"/></entry>
				<entry><link href="https://github.com/owner/repo/releases/tag/v2"/></entry>
			</feed>`), nil
		case "/repos/owner/repo/releases/tags/v3?":
			hydrated++
			return githubJSONResponse(`{
				"id":3,"tag_name":"v3","published_at":"2026-03-01T00:00:00Z","assets":[]
			}`), nil
		default:
			return nil, errors.New("unexpected GitHub request " + key)
		}
	})
	client := HTTPGitHubClient{Client: &http.Client{Transport: transport}}

	got, err := client.ListReleasesLimited(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TagName != "v3" || hydrated != 1 {
		t.Fatalf("limited Atom recovery got=%+v hydrated=%d", got, hydrated)
	}
}

func TestHTTPGitHubClientDoesNotHydrateAtomAfterRateLimit(t *testing.T) {
	apiRequests := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path + "?" + req.URL.RawQuery
		switch key {
		case "/repos/owner/repo/releases?per_page=100":
			apiRequests++
			return &http.Response{StatusCode: http.StatusForbidden,
				Status: "403 Forbidden", Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
		case "/owner/repo/releases.atom?":
			return githubAtomResponse(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">
				<entry><link href="https://github.com/owner/repo/releases/tag/v3"/></entry>
				<entry><link href="https://github.com/owner/repo/releases/tag/v2"/></entry>
			</feed>`), nil
		default:
			apiRequests++
			return nil, errors.New("Atom hydration must not run after rate limiting: " + key)
		}
	})
	client := HTTPGitHubClient{Client: &http.Client{Transport: transport}}

	_, err := client.ListReleasesLimited(context.Background(), "owner/repo", 3)
	if !errors.Is(err, errGitHubRateLimited) {
		t.Fatalf("expected rate-limit error, got %v", err)
	}
	if apiRequests != 1 {
		t.Fatalf("API request count got=%d want=1", apiRequests)
	}
}
