package mirrorsync

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

func (c HTTPGitHubClient) releaseByTag(ctx context.Context, client *http.Client,
	repo, tag string) (GitHubRelease, bool, error) {
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
