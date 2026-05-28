package mirrorsync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type GitHubClient interface {
	ListReleases(ctx context.Context, repo string) ([]GitHubRelease, error)
}

type HTTPGitHubClient struct {
	Client *http.Client
	Token  string
}

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

func (c HTTPGitHubClient) ListReleases(ctx context.Context, repo string) ([]GitHubRelease, error) {
	client := c.Client
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.github.com/repos/"+repo+"/releases?per_page=100", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Release 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GitHub 限频或拒绝访问：%s", resp.Status)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub Release 响应异常：%s", resp.Status)
	}
	var raw []struct {
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
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("解析 GitHub Release 失败：%w", err)
	}
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
	return items, nil
}
