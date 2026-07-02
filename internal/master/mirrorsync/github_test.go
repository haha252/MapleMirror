package mirrorsync

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHTTPGitHubClientBackfillsReleaseMissingFromListUsingAtom(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path + "?" + req.URL.RawQuery
		switch key {
		case "/repos/owner/repo/releases?per_page=100":
			return githubJSONResponse(`[
				{"id":1,"tag_name":"v1","draft":false,"prerelease":false,
				 "published_at":"2026-01-01T00:00:00Z","assets":[]}
			]`), nil
		case "/owner/repo/releases.atom?":
			return githubAtomResponse(`<?xml version="1.0" encoding="UTF-8"?>
				<feed xmlns="http://www.w3.org/2005/Atom">
				  <entry><link href="https://github.com/owner/repo/releases/tag/v2"/></entry>
				  <entry><link href="https://github.com/owner/repo/releases/tag/v1"/></entry>
				</feed>`), nil
		case "/repos/owner/repo/releases/tags/v2?":
			return githubJSONResponse(`{
				"id":2,"tag_name":"v2","draft":false,"prerelease":false,
				"published_at":"2026-02-01T00:00:00Z",
				"assets":[{"id":20,"name":"app.zip","size":12,
					"browser_download_url":"https://example.invalid/app.zip",
					"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]
			}`), nil
		default:
			t.Fatalf("unexpected GitHub request %s", key)
			return nil, nil
		}
	})
	client := HTTPGitHubClient{Client: &http.Client{Transport: transport}}

	got, err := client.ListReleases(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("release count got=%d want=2 releases=%+v", len(got), got)
	}
	if got[0].TagName != "v1" || got[1].TagName != "v2" {
		t.Fatalf("release tags got=%q,%q want v1,v2", got[0].TagName, got[1].TagName)
	}
	if len(got[1].Assets) != 1 || got[1].Assets[0].Name != "app.zip" {
		t.Fatalf("backfilled asset not converted: %+v", got[1].Assets)
	}
}

func TestHTTPGitHubClientKeepsListResultWhenAtomFails(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path + "?" + req.URL.RawQuery
		switch key {
		case "/repos/owner/repo/releases?per_page=100":
			return githubJSONResponse(`[
				{"id":1,"tag_name":"v1","draft":false,"prerelease":false,
				 "published_at":"2026-01-01T00:00:00Z","assets":[]}
			]`), nil
		case "/owner/repo/releases.atom?":
			return &http.Response{StatusCode: http.StatusBadGateway,
				Status: "502 Bad Gateway", Body: io.NopCloser(strings.NewReader("bad gateway"))}, nil
		default:
			t.Fatalf("unexpected GitHub request %s", key)
			return nil, nil
		}
	})
	client := HTTPGitHubClient{Client: &http.Client{Transport: transport}}

	got, err := client.ListReleases(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TagName != "v1" {
		t.Fatalf("fallback failure should preserve release list: %+v", got)
	}
}

func TestHTTPGitHubClientRecoversFromListFailureUsingAtom(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path + "?" + req.URL.RawQuery
		switch key {
		case "/repos/owner/repo/releases?per_page=100":
			return &http.Response{StatusCode: http.StatusBadGateway,
				Status: "502 Bad Gateway", Body: io.NopCloser(strings.NewReader("bad gateway"))}, nil
		case "/owner/repo/releases.atom?":
			return githubAtomResponse(`<?xml version="1.0" encoding="UTF-8"?>
				<feed xmlns="http://www.w3.org/2005/Atom">
				  <entry><link href="https://github.com/owner/repo/releases/tag/v2"/></entry>
				</feed>`), nil
		case "/repos/owner/repo/releases/tags/v2?":
			return githubJSONResponse(`{
				"id":2,"tag_name":"v2","draft":false,"prerelease":false,
				"published_at":"2026-02-01T00:00:00Z","assets":[]
			}`), nil
		default:
			return nil, errors.New("unexpected GitHub request " + key)
		}
	})
	client := HTTPGitHubClient{Client: &http.Client{Transport: transport}}

	got, err := client.ListReleases(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TagName != "v2" {
		t.Fatalf("Atom should recover the current release: %+v", got)
	}
}

func TestHTTPGitHubClientReservesTimeToRecoverFromStalledList(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		key := req.URL.Path + "?" + req.URL.RawQuery
		switch key {
		case "/repos/owner/repo/releases?per_page=100":
			<-req.Context().Done()
			return nil, req.Context().Err()
		case "/owner/repo/releases.atom?":
			return githubAtomResponse(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom">
				<entry><link href="https://github.com/owner/repo/releases/tag/v2"/></entry>
			</feed>`), nil
		case "/repos/owner/repo/releases/tags/v2?":
			return githubJSONResponse(`{
				"id":2,"tag_name":"v2","published_at":"2026-02-01T00:00:00Z","assets":[]
			}`), nil
		default:
			return nil, errors.New("unexpected GitHub request " + key)
		}
	})
	client := HTTPGitHubClient{
		Client: &http.Client{Transport: transport}, Timeout: 100 * time.Millisecond,
	}

	got, err := client.ListReleases(context.Background(), "owner/repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TagName != "v2" {
		t.Fatalf("Atom should recover after the list probe times out: %+v", got)
	}
}

func TestHTTPGitHubClientSendsTokenOnlyToAPI(t *testing.T) {
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Host {
		case "api.github.com":
			if got := req.Header.Get("Authorization"); got != "Bearer secret" {
				return nil, errors.New("API Authorization header missing")
			}
			return githubJSONResponse(`[]`), nil
		case "github.com":
			if got := req.Header.Get("Authorization"); got != "" {
				return nil, errors.New("Token leaked to Atom request")
			}
			return githubAtomResponse(`<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom"/>`), nil
		default:
			return nil, errors.New("unexpected GitHub host " + req.URL.Host)
		}
	})
	client := HTTPGitHubClient{Client: &http.Client{Transport: transport}, Token: "secret"}
	if _, err := client.ListReleases(context.Background(), "owner/repo"); err != nil {
		t.Fatal(err)
	}
}

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

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func githubJSONResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func githubAtomResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
