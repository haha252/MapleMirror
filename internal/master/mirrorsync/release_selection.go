package mirrorsync

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

func observedReleases(releases []ResourceVersion, includePrerelease bool) []ResourceVersion {
	out := make([]ResourceVersion, 0, len(releases))
	for _, release := range releases {
		if release.Draft || (release.Prerelease && !includePrerelease) {
			continue
		}
		out = append(out, release)
	}
	return out
}

func selectReleases(releases []ResourceVersion, project config.Project) ([]ResourceVersion, error) {
	selected := make([]ResourceVersion, 0, len(releases))
	for _, rel := range releases {
		if rel.Draft || (rel.Prerelease && !project.IncludePrerelease) {
			continue
		}
		ok, err := releaseHasMirrorableAsset(project, rel)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		selected = append(selected, rel)
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return newerResourceVersion(selected[i], selected[j])
	})
	if keep := project.RetainVersions; keep > 0 && len(selected) > keep {
		selected = selected[:keep]
	}
	return selected, nil
}

func releaseHasMirrorableAsset(project config.Project, release ResourceVersion) (bool, error) {
	for _, asset := range release.Assets {
		allowed, _, err := assetAllowed(asset.FileName, project.AssetInclude, project.AssetExclude)
		if err != nil {
			return false, err
		}
		if !allowed {
			continue
		}
		if _, err := normalizeDigest(asset.Digest); err != nil {
			continue
		}
		return true, nil
	}
	return false, nil
}

func newerResourceVersion(a, b ResourceVersion) bool {
	if a.PublishedAt.Equal(b.PublishedAt) {
		return a.NumericID > b.NumericID
	}
	return a.PublishedAt.After(b.PublishedAt)
}

func reconcileKnownObservedReleases(ctx context.Context, tx *sql.Tx, project config.Project, observed, retained []ResourceVersion, now string, logger *logging.Logger) (ScanSummary, error) {
	retainedIDs := make(map[int64]struct{}, len(retained))
	for _, release := range retained {
		retainedIDs[release.NumericID] = struct{}{}
	}
	var summary ScanSummary
	for _, release := range observed {
		if _, ok := retainedIDs[release.NumericID]; ok {
			continue
		}
		releaseID := fmt.Sprintf("%s:%d", project.ID, release.NumericID)
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM releases WHERE id = ?`, releaseID).Scan(&exists); err != nil {
			return summary, err
		}
		if exists == 0 {
			continue
		}
		if _, err := tx.Exec(`UPDATE releases SET tag_name=?, prerelease=?, published_at=?,
			source_type=?, source_release_key=? WHERE id=?`, release.Version, boolInt(release.Prerelease),
			release.PublishedAt.UTC().Format(time.RFC3339Nano), release.SourceType, release.SourceReleaseKey, releaseID); err != nil {
			return summary, err
		}
		accepted, rejected, err := writeAssets(ctx, tx, project, releaseID, release, logger, now)
		summary.AcceptedAssets += accepted
		summary.RejectedAssets += rejected
		if err != nil {
			return summary, err
		}
	}
	return summary, nil
}
