package mirrorsync

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const recentAtomReleaseProbeLimit = 10
const maxGitHubAtomFeedBytes = 2 << 20

type githubReleaseFeed struct {
	Entries []struct {
		Links []struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
	} `xml:"entry"`
}

func (c HTTPGitHubClient) listReleaseFeed(ctx context.Context, client *http.Client, repo string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://github.com/"+repo+"/releases.atom", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/atom+xml")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 GitHub Release Atom 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitHub Release Atom 响应异常：%s", resp.Status)
	}
	var feed githubReleaseFeed
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxGitHubAtomFeedBytes)).Decode(&feed); err != nil {
		return nil, fmt.Errorf("解析 GitHub Release Atom 失败：%w", err)
	}
	prefix := "/" + strings.Trim(repo, "/") + "/releases/tag/"
	tags := make([]string, 0, min(len(feed.Entries), recentAtomReleaseProbeLimit))
	for _, entry := range feed.Entries {
		for _, link := range entry.Links {
			u, err := url.Parse(link.Href)
			if err != nil || !strings.HasPrefix(u.Path, prefix) {
				continue
			}
			tag := strings.TrimSpace(strings.TrimPrefix(u.Path, prefix))
			if tag != "" {
				tags = append(tags, tag)
			}
			break
		}
		if len(tags) >= recentAtomReleaseProbeLimit {
			break
		}
	}
	return tags, nil
}
