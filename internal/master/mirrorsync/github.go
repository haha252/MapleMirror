package mirrorsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"mirror-server/internal/logging"
)

type GitHubClient interface {
	ListReleases(ctx context.Context, repo string) ([]GitHubRelease, error)
}

type HTTPGitHubClient struct {
	Client  *http.Client
	Token   string
	Timeout time.Duration
	Logger  *logging.Logger
}

const DefaultGitHubClientTimeout = 2 * time.Minute

var (
	errGitHubNotFound    = errors.New("GitHub 资源不存在")
	errGitHubRateLimited = errors.New("GitHub 限频或拒绝访问")
)

type GitHubRelease struct {
	ID          int64
	TagName     string
	Prerelease  bool
	Draft       bool
	PublishedAt time.Time
	Assets      []GitHubAsset
}

type GitHubAsset struct {
	ID         int64
	Name       string
	Size       int64
	URL        string
	Digest     string
	BrowserURL string
}

type githubReleasePayload struct {
	ID          int64     `json:"id"`
	TagName     string    `json:"tag_name"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []struct {
		ID                 int64  `json:"id"`
		Name               string `json:"name"`
		Size               int64  `json:"size"`
		URL                string `json:"url"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

func (c HTTPGitHubClient) ListReleases(ctx context.Context, repo string) ([]GitHubRelease, error) {
	return c.listReleasesWithLimit(ctx, repo, 0)
}

func (c HTTPGitHubClient) ListReleasesLimited(ctx context.Context, repo string,
	maxReleases int) ([]GitHubRelease, error) {
	return c.listReleasesWithLimit(ctx, repo, maxReleases)
}

func (c HTTPGitHubClient) listReleasesWithLimit(ctx context.Context, repo string,
	maxReleases int) ([]GitHubRelease, error) {
	timeout := c.requestTimeout()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	probeTimeout := timeout / 2
	if probeTimeout <= 0 {
		probeTimeout = timeout
	}
	probeCtx, cancelProbes := context.WithTimeout(ctx, probeTimeout)
	defer cancelProbes()
	type listResult struct {
		releases []GitHubRelease
		err      error
	}
	type feedResult struct {
		tags []string
		err  error
	}
	listCh := make(chan listResult, 1)
	feedCh := make(chan feedResult, 1)
	go func() {
		releases, err := c.listReleases(probeCtx, client, repo)
		listCh <- listResult{releases: releases, err: err}
	}()
	go func() {
		tags, err := c.listReleaseFeed(probeCtx, client, repo)
		feedCh <- feedResult{tags: tags, err: err}
	}()
	listed, fed := <-listCh, <-feedCh
	cancelProbes()
	if fed.err != nil {
		if listed.err != nil {
			return nil, errors.Join(listed.err, fed.err)
		}
		c.logProbeWarning(ctx, repo, "GitHub Release Atom 探测失败，使用 REST API 结果", fed.err)
		return listed.releases, nil
	}
	// A rate-limited API cannot hydrate Atom entries either. Avoid turning one
	// rejected list request into one rejected request per feed entry.
	if errors.Is(listed.err, errGitHubRateLimited) {
		return nil, listed.err
	}
	releases, recovered, hydrateErr := c.appendFeedReleases(ctx, client, repo,
		listed.releases, fed.tags, maxReleases)
	if listed.err != nil {
		if recovered == 0 || hydrateErr != nil {
			return nil, errors.Join(listed.err, hydrateErr)
		}
		c.logProbeWarning(ctx, repo, "GitHub Release REST 列表失败，已通过 Atom 恢复", listed.err)
	} else if hydrateErr != nil {
		c.logProbeWarning(ctx, repo, "GitHub Release Atom 补取不完整，保留 REST API 结果", hydrateErr)
	}
	return releases, nil
}

func (c HTTPGitHubClient) logProbeWarning(ctx context.Context, repo, message string, err error) {
	if c.Logger != nil {
		c.Logger.Warn(ctx, message, slog.String("repository", repo), slog.String("error", err.Error()))
	}
}

func (c HTTPGitHubClient) listReleases(ctx context.Context, client *http.Client, repo string) ([]GitHubRelease, error) {
	var raw []githubReleasePayload
	if err := c.getJSON(ctx, client, "https://api.github.com/repos/"+repo+"/releases?per_page=100", &raw); err != nil {
		return nil, err
	}
	return convertGitHubReleases(raw), nil
}

func (c HTTPGitHubClient) appendFeedReleases(ctx context.Context, client *http.Client, repo string,
	releases []GitHubRelease, tags []string, maxReleases int) ([]GitHubRelease, int, error) {
	seenTags := make(map[string]struct{}, len(releases))
	seenIDs := make(map[int64]struct{}, len(releases))
	for _, release := range releases {
		seenTags[release.TagName] = struct{}{}
		seenIDs[release.ID] = struct{}{}
	}
	recovered := 0
	existingReleases := len(releases)
	hydrateAttempts := 0
	var hydrateErr error
	for _, tag := range tags {
		if maxReleases > 0 && existingReleases+hydrateAttempts >= maxReleases {
			break
		}
		name := strings.TrimSpace(tag)
		if name == "" {
			continue
		}
		if _, ok := seenTags[name]; ok {
			continue
		}
		seenTags[name] = struct{}{}
		hydrateAttempts++
		release, ok, err := c.releaseByTag(ctx, client, repo, name)
		if err != nil {
			hydrateErr = errors.Join(hydrateErr, fmt.Errorf("补取 GitHub Release %q 失败：%w", name, err))
			if errors.Is(err, errGitHubRateLimited) {
				break
			}
			continue
		}
		if !ok {
			hydrateErr = errors.Join(hydrateErr, fmt.Errorf("Atom 中的 GitHub Release %q 不存在", name))
			continue
		}
		if _, ok := seenIDs[release.ID]; ok {
			continue
		}
		releases = append(releases, release)
		recovered++
		seenTags[release.TagName] = struct{}{}
		seenIDs[release.ID] = struct{}{}
	}
	return releases, recovered, hydrateErr
}

func (c HTTPGitHubClient) getJSON(ctx context.Context, client *http.Client, endpoint string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求 GitHub Release 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("%w：%s", errGitHubRateLimited, resp.Status)
	}
	if resp.StatusCode == http.StatusNotFound {
		return errGitHubNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("GitHub Release 响应异常：%s", resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		return fmt.Errorf("解析 GitHub Release 失败：%w", err)
	}
	return nil
}

func convertGitHubReleases(raw []githubReleasePayload) []GitHubRelease {
	items := make([]GitHubRelease, 0, len(raw))
	for _, r := range raw {
		rel := GitHubRelease{ID: r.ID, TagName: r.TagName, Prerelease: r.Prerelease,
			Draft: r.Draft, PublishedAt: r.PublishedAt}
		for _, a := range r.Assets {
			url := a.BrowserDownloadURL
			if strings.TrimSpace(url) == "" {
				url = a.URL
			}
			rel.Assets = append(rel.Assets, GitHubAsset{ID: a.ID, Name: a.Name,
				Size: a.Size, URL: url, BrowserURL: a.BrowserDownloadURL, Digest: a.Digest})
		}
		items = append(items, rel)
	}
	return items
}
