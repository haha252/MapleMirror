package mirrorsync

import (
	"context"
	"fmt"
	"time"

	"mirror-server/internal/config"
)

const sourceTypeGitHubReleases = "github_releases"

type ResourceSource interface {
	ListResourceVersions(ctx context.Context, project config.Project) ([]ResourceVersion, error)
}

type GitHubReleaseSource struct {
	Client GitHubClient
}

type ResourceVersion struct {
	SourceType       string
	SourceReleaseKey string
	NumericID        int64
	Version          string
	Prerelease       bool
	Draft            bool
	PublishedAt      time.Time
	Assets           []ResourceCandidate
}

type ResourceCandidate struct {
	SourceType     string
	SourceAssetKey string
	NumericID      int64
	FileName       string
	SizeBytes      int64
	DownloadURL    string
	Digest         string
	Metadata       map[string]string
}

func (s GitHubReleaseSource) ListResourceVersions(ctx context.Context, project config.Project) ([]ResourceVersion, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("GitHub Release 来源未配置客户端")
	}
	releases, err := s.Client.ListReleases(ctx, project.Repository)
	if err != nil {
		return nil, err
	}
	out := make([]ResourceVersion, 0, len(releases))
	for _, release := range releases {
		item := ResourceVersion{
			SourceType:       sourceTypeGitHubReleases,
			SourceReleaseKey: fmt.Sprint(release.ID),
			NumericID:        release.ID,
			Version:          release.TagName,
			Prerelease:       release.Prerelease,
			Draft:            release.Draft,
			PublishedAt:      release.PublishedAt,
			Assets:           make([]ResourceCandidate, 0, len(release.Assets)),
		}
		for _, asset := range release.Assets {
			item.Assets = append(item.Assets, ResourceCandidate{
				SourceType:     sourceTypeGitHubReleases,
				SourceAssetKey: fmt.Sprint(asset.ID),
				NumericID:      asset.ID,
				FileName:       asset.Name,
				SizeBytes:      asset.Size,
				DownloadURL:    asset.URL,
				Digest:         asset.Digest,
				Metadata: map[string]string{
					"browser_url": asset.BrowserURL,
				},
			})
		}
		out = append(out, item)
	}
	return out, nil
}
