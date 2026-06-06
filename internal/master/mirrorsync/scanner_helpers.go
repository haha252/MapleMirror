package mirrorsync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

func writeAssets(ctx context.Context, tx *sql.Tx, project config.Project, releaseID string, assets []GitHubAsset, logger *logging.Logger, now string) (int, int, error) {
	archRE, err := compileArchitectureRegex(project)
	if err != nil {
		return 0, 0, err
	}
	systemRE, err := compileSystemRegex(project)
	if err != nil {
		return 0, 0, err
	}
	var accepted, rejected int
	for _, asset := range assets {
		allowed, reason, err := assetAllowed(asset.Name, project.AssetInclude, project.AssetExclude)
		if err != nil {
			return accepted, rejected, err
		}
		if !allowed {
			if logger != nil {
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.Name),
					slog.String("reason", reason))
			}
			rejected++
			continue
		}
		digest, err := normalizeDigest(asset.Digest)
		if err != nil {
			if logger != nil {
				logger.Debug(ctx, "资产未进入镜像流程",
					slog.String("project_id", project.ID),
					slog.String("release_id", releaseID),
					slog.String("asset_name", asset.Name),
					slog.String("reason", "digest invalid"))
			}
			rejected++
			continue
		}
		arch := assetArchitecture(asset.Name, archRE)
		system := assetSystem(asset.Name, systemRE)
		assetID := fmt.Sprintf("%s:%d", releaseID, asset.ID)
		if err := markInventoryStaleOnAssetChange(ctx, tx, assetID, digest, asset.Size, now); err != nil {
			return accepted, rejected, err
		}
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

func markInventoryStaleOnAssetChange(ctx context.Context, tx *sql.Tx, assetID, digest string, size int64, now string) error {
	var oldDigest string
	var oldSize int64
	err := tx.QueryRowContext(ctx, `SELECT digest_sha256, size_bytes FROM assets
		WHERE id = ?`, assetID).Scan(&oldDigest, &oldSize)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if oldDigest == digest && oldSize == size {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE node_inventory SET state = 'mismatch',
		verified_at = ? WHERE asset_id = ? AND state = 'verified'`, now, assetID)
	return err
}

func supersedeDuplicatePublicPaths(ctx context.Context, tx *sql.Tx, projectID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE assets SET service_state = 'superseded'
		WHERE id IN (
			SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
			WHERE r.project_id = ? AND r.selected = 1 AND a.service_state = 'candidate'
			AND EXISTS (
				SELECT 1 FROM assets newer JOIN releases nr ON nr.id = newer.release_id
				WHERE nr.project_id = r.project_id AND nr.selected = 1
				AND newer.service_state = 'candidate'
				AND nr.tag_name = r.tag_name AND newer.file_name = a.file_name
				AND (
					nr.published_at > r.published_at
					OR (nr.published_at = r.published_at
						AND nr.github_release_id > r.github_release_id)
					OR (nr.published_at = r.published_at
						AND nr.github_release_id = r.github_release_id
						AND newer.github_asset_id > a.github_asset_id)
				)
			)
		)`, projectID)
	return err
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

func projectHash(project config.Project) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		project.ID, project.Repository,
		fmt.Sprint(project.Enabled), fmt.Sprint(project.RetainVersions),
		fmt.Sprint(project.IncludePrerelease), fmt.Sprint(project.DownloadMultiplier),
		assetRulesHash(project.AssetInclude),
		assetRulesHash(project.AssetExclude),
		project.ArchitectureRegex,
		fmt.Sprint(project.ArchitectureMatchEnabled),
		fmt.Sprint(project.SystemMatchEnabled),
		project.SystemRegex,
	}, "|")))
	return hex.EncodeToString(sum[:])
}

func compileArchitectureRegex(project config.Project) (*regexp.Regexp, error) {
	if !project.ArchitectureMatchEnabled {
		return nil, nil
	}
	return regexp.Compile(project.ArchitectureRegex)
}

func assetArchitecture(name string, archRE *regexp.Regexp) string {
	if archRE == nil {
		return ""
	}
	matches := archRE.FindStringSubmatch(name)
	if len(matches) == 0 {
		return "None"
	}
	return matches[len(matches)-1]
}

func compileSystemRegex(project config.Project) (*regexp.Regexp, error) {
	if !project.SystemMatchEnabled {
		return nil, nil
	}
	return regexp.Compile(project.SystemRegex)
}

func assetSystem(name string, systemRE *regexp.Regexp) string {
	if systemRE == nil {
		return ""
	}
	matches := systemRE.FindStringSubmatch(name)
	if len(matches) == 0 {
		return "None"
	}
	system, ok := normalizeSystem(matches[len(matches)-1])
	if !ok {
		return "None"
	}
	return system
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
