package mirrorsync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"mirror-server/internal/config"
)

type Scanner struct {
	Store  Store
	GitHub GitHubClient
}

func (s Scanner) Scan(ctx context.Context, projects config.Projects, projectID, requestID string) (ScanSummary, error) {
	scanID, err := s.Store.StartScan(ctx, projectID, requestID)
	if err != nil {
		return ScanSummary{}, err
	}
	summary := ScanSummary{ScanID: scanID, ProjectID: projectID, RequestID: requestID}
	err = s.scan(ctx, projects, projectID, &summary)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	_ = s.Store.FinishScan(ctx, scanID, summary, errText)
	return summary, err
}

func (s Scanner) scan(ctx context.Context, projects config.Projects, projectID string, summary *ScanSummary) error {
	for _, project := range projects.Projects {
		if !project.Enabled || (projectID != "" && project.ID != projectID) {
			continue
		}
		releases, err := s.GitHub.ListReleases(ctx, project.Repository)
		if err != nil {
			return err
		}
		selected := selectReleases(releases, project.IncludePrerelease, project.RetainVersions)
		projectSummary, err := s.writeProject(ctx, project, selected)
		if err != nil {
			return err
		}
		summary.SelectedReleases += projectSummary.SelectedReleases
		summary.AcceptedAssets += projectSummary.AcceptedAssets
		summary.RejectedAssets += projectSummary.RejectedAssets
	}
	return nil
}

func (s Scanner) writeProject(ctx context.Context, project config.Project, releases []GitHubRelease) (ScanSummary, error) {
	tx, err := s.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return ScanSummary{}, err
	}
	defer tx.Rollback()
	now := nowText()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name,
		repository = excluded.repository, enabled = excluded.enabled,
		retain_versions = excluded.retain_versions,
		include_prerelease = excluded.include_prerelease,
		download_multiplier = excluded.download_multiplier,
		config_hash = excluded.config_hash, updated_at = excluded.updated_at`,
		project.ID, project.Name, project.Repository, boolInt(project.Enabled),
		project.RetainVersions, boolInt(project.IncludePrerelease),
		project.DownloadMultiplier, projectHash(project), now); err != nil {
		return ScanSummary{}, err
	}
	summary, err := writeReleases(ctx, tx, project, releases, now)
	if err != nil {
		return ScanSummary{}, err
	}
	if err := rebuildTargetInventory(ctx, tx, project.ID, now); err != nil {
		return ScanSummary{}, err
	}
	if err := generateTasks(ctx, tx, now); err != nil {
		return ScanSummary{}, err
	}
	return summary, tx.Commit()
}

func writeReleases(ctx context.Context, tx *sql.Tx, project config.Project, releases []GitHubRelease, now string) (ScanSummary, error) {
	var summary ScanSummary
	_, _ = tx.ExecContext(ctx, `UPDATE releases SET selected = 0 WHERE project_id = ?`, project.ID)
	for _, rel := range releases {
		summary.SelectedReleases++
		releaseID := fmt.Sprintf("%s:%d", project.ID, rel.ID)
		_, err := tx.ExecContext(ctx, `INSERT INTO releases
			(id, project_id, github_release_id, tag_name, prerelease, published_at, selected, created_at)
			VALUES (?, ?, ?, ?, ?, ?, 1, ?)
			ON CONFLICT(project_id, github_release_id) DO UPDATE SET tag_name = excluded.tag_name,
			prerelease = excluded.prerelease, published_at = excluded.published_at, selected = 1`,
			releaseID, project.ID, rel.ID, rel.TagName, boolInt(rel.Prerelease),
			rel.PublishedAt.UTC().Format(time.RFC3339Nano), now)
		if err != nil {
			return summary, err
		}
		accepted, rejected, err := writeAssets(ctx, tx, project, releaseID, rel.Assets, now)
		summary.AcceptedAssets += accepted
		summary.RejectedAssets += rejected
		if err != nil {
			return summary, err
		}
	}
	return summary, nil
}

func writeAssets(ctx context.Context, tx *sql.Tx, project config.Project, releaseID string, assets []GitHubAsset, now string) (int, int, error) {
	archRE, err := regexp.Compile(project.ArchitectureRegex)
	if err != nil {
		return 0, 0, err
	}
	var accepted, rejected int
	for _, asset := range assets {
		if !assetAllowed(asset.Name, project.AssetInclude, project.AssetExclude) {
			rejected++
			continue
		}
		digest, err := normalizeDigest(asset.Digest)
		matches := archRE.FindStringSubmatch(asset.Name)
		if err != nil || len(matches) == 0 {
			rejected++
			continue
		}
		arch := matches[len(matches)-1]
		assetID := fmt.Sprintf("%s:%d", releaseID, asset.ID)
		_, err = tx.ExecContext(ctx, `INSERT INTO assets
			(id, release_id, github_asset_id, file_name, architecture, size_bytes,
			source_url, digest_sha256, service_state, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'candidate', ?)
			ON CONFLICT(release_id, github_asset_id) DO UPDATE SET
			file_name = excluded.file_name, architecture = excluded.architecture,
			size_bytes = excluded.size_bytes, source_url = excluded.source_url,
			digest_sha256 = excluded.digest_sha256, service_state = 'candidate'`,
			assetID, releaseID, asset.ID, asset.Name, arch, asset.Size,
			asset.URL, digest, now)
		if err != nil {
			return accepted, rejected, err
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
	sum := sha256.Sum256([]byte(strings.Join([]string{project.ID, project.Repository,
		fmt.Sprint(project.Enabled), fmt.Sprint(project.RetainVersions),
		fmt.Sprint(project.IncludePrerelease), fmt.Sprint(project.DownloadMultiplier),
		strings.Join(project.AssetInclude, ","), strings.Join(project.AssetExclude, ","),
		project.ArchitectureRegex}, "|")))
	return hex.EncodeToString(sum[:])
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
