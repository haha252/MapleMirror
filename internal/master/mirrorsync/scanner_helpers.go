package mirrorsync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

func writeAssets(ctx context.Context, tx *sql.Tx, project config.Project, releaseID string, assets []GitHubAsset, logger *logging.Logger, now string) (int, int, error) {
	archRE, err := regexp.Compile(project.ArchitectureRegex)
	if err != nil {
		return 0, 0, err
	}
	systemRE, err := compileSystemRegex(project)
	if err != nil {
		return 0, 0, err
	}
	var accepted, rejected int
	for _, asset := range assets {
		if !assetAllowed(asset.Name, project.AssetInclude, project.AssetExclude) {
			if logger != nil {
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.Name),
					slog.String("reason", "asset exclude"))
			}
			rejected++
			continue
		}
		digest, err := normalizeDigest(asset.Digest)
		matches := archRE.FindStringSubmatch(asset.Name)
		if err != nil || len(matches) == 0 {
			if logger != nil {
				reason := "architecture mismatch"
				if err != nil {
					reason = "digest invalid"
				}
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.Name),
					slog.String("reason", reason))
			}
			rejected++
			continue
		}
		arch := matches[len(matches)-1]
		system, ok := assetSystem(asset.Name, systemRE)
		if !ok {
			if logger != nil {
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.Name),
					slog.String("reason", "system mismatch"))
			}
			rejected++
			continue
		}
		assetID := fmt.Sprintf("%s:%d", releaseID, asset.ID)
		_, err = tx.ExecContext(ctx, `INSERT INTO assets
			(id, release_id, github_asset_id, file_name, architecture, system, size_bytes,
			source_url, digest_sha256, service_state, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'candidate', ?)
			ON CONFLICT(release_id, github_asset_id) DO UPDATE SET
			file_name = excluded.file_name, architecture = excluded.architecture,
			system = excluded.system,
			size_bytes = excluded.size_bytes, source_url = excluded.source_url,
			digest_sha256 = excluded.digest_sha256, service_state = 'candidate'`,
			assetID, releaseID, asset.ID, asset.Name, arch, system, asset.Size,
			asset.URL, digest, now)
		if err != nil {
			return accepted, rejected, err
		}
		if logger != nil {
			logger.Debug(ctx, "资产已进入镜像候选",
				slog.String("project_id", project.ID),
				slog.String("release_id", releaseID),
				slog.String("asset_id", assetID),
				slog.String("asset_name", asset.Name),
				slog.String("architecture", arch),
				slog.String("system", system),
				slog.String("digest_sha256", digest))
		}
		accepted++
	}
	return accepted, rejected, nil
}

func selectReleases(releases []GitHubRelease, includePrerelease bool, keep int) []GitHubRelease {
	var selected []GitHubRelease
	for _, rel := range releases {
		if rel.Draft || (rel.Prerelease && !includePrerelease) {
			continue
		}
		selected = append(selected, rel)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].PublishedAt.Equal(selected[j].PublishedAt) {
			return selected[i].ID > selected[j].ID
		}
		return selected[i].PublishedAt.After(selected[j].PublishedAt)
	})
	if keep > 0 && len(selected) > keep {
		selected = selected[:keep]
	}
	return selected
}

func assetAllowed(name string, includes, excludes []string) bool {
	for _, pattern := range excludes {
		if matched, _ := path.Match(pattern, name); matched {
			return false
		}
	}
	if len(includes) == 0 {
		return true
	}
	for _, pattern := range includes {
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

func projectHash(project config.Project) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		project.ID, project.Repository,
		fmt.Sprint(project.Enabled), fmt.Sprint(project.RetainVersions),
		fmt.Sprint(project.IncludePrerelease), fmt.Sprint(project.DownloadMultiplier),
		strings.Join(project.AssetInclude, ","),
		strings.Join(project.AssetExclude, ","),
		project.ArchitectureRegex,
		fmt.Sprint(project.SystemMatchEnabled),
		project.SystemRegex,
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func compileSystemRegex(project config.Project) (*regexp.Regexp, error) {
	if !project.SystemMatchEnabled {
		return nil, nil
	}
	return regexp.Compile(project.SystemRegex)
}

func assetSystem(name string, systemRE *regexp.Regexp) (string, bool) {
	if systemRE == nil {
		return "", true
	}
	matches := systemRE.FindStringSubmatch(name)
	if len(matches) == 0 {
		return "", false
	}
	return normalizeSystem(matches[len(matches)-1])
}

func normalizeSystem(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "win", "windows", "win32", "win64":
		return "win", true
	case "linux":
		return "linux", true
	case "darwin", "macos", "osx":
		return "darwin", true
	default:
		return "", false
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
