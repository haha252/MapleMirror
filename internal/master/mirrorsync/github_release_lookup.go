package mirrorsync

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func (c HTTPGitHubClient) releaseByTag(ctx context.Context, client *http.Client, repo, tag string) (GitHubRelease, bool, error) {
	var raw githubReleasePayload
	err := c.getJSON(ctx, client,
		"https://api.github.com/repos/"+repo+"/releases/tags/"+url.PathEscape(tag), &raw)
	if errors.Is(err, errGitHubNotFound) {
		return GitHubRelease{}, false, nil
	}
	if err != nil {
		return GitHubRelease{}, false, err
	}
	releases := convertGitHubReleases([]githubReleasePayload{raw})
	if len(releases) == 0 {
		return GitHubRelease{}, false, nil
	}
	return releases[0], true, nil
}

func (c HTTPGitHubClient) ReleaseExists(ctx context.Context, repo string, releaseID int64) (bool, error) {
	timeout := c.requestTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	var raw githubReleasePayload
	err := c.getJSON(ctx, client, "https://api.github.com/repos/"+repo+"/releases/"+
		strconv.FormatInt(releaseID, 10), &raw)
	if errors.Is(err, errGitHubNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if raw.ID != releaseID {
		return false, fmt.Errorf("GitHub Release ID 响应异常：got=%d want=%d", raw.ID, releaseID)
	}
	return true, nil
}
