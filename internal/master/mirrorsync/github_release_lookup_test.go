package mirrorsync

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestHTTPGitHubClientReleaseExistsOnlyTreats404AsDeleted(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		exists bool
	}{
		{name: "exists", status: http.StatusOK, body: `{"id":90}`, exists: true},
		{name: "deleted", status: http.StatusNotFound, body: `{}`, exists: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path != "/repos/owner/repo/releases/90" {
					return nil, errors.New("unexpected GitHub request " + req.URL.Path)
				}
				return githubLookupResponse(tt.status, tt.body), nil
			})
			client := HTTPGitHubClient{Client: &http.Client{Transport: transport}}
			exists, err := client.ReleaseExists(context.Background(), "owner/repo", 90)
			if err != nil {
				t.Fatal(err)
			}
			if exists != tt.exists {
				t.Fatalf("exists=%v want=%v", exists, tt.exists)
			}
		})
	}
}

func githubLookupResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
