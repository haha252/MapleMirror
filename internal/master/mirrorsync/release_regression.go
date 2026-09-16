package mirrorsync

import (
	"context"
	"fmt"
	"sort"
	"time"
)

func (s Store) MissingSelectedReleases(ctx context.Context, projectID string,
	incoming, observed []ResourceVersion, keep int) ([]ResourceVersion, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.github_release_id, r.tag_name, r.published_at
		FROM releases r WHERE r.project_id = ? AND r.selected = 1
		AND EXISTS (SELECT 1 FROM assets a WHERE a.release_id = r.id
			AND a.service_state IN ('candidate','pending','active','superseded'))
		ORDER BY r.published_at DESC, r.github_release_id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var previous []ResourceVersion
	for rows.Next() {
		var release ResourceVersion
		var published string
		if err := rows.Scan(&release.NumericID, &release.Version, &published); err != nil {
			return nil, err
		}
		release.PublishedAt, err = time.Parse(time.RFC3339Nano, published)
		if err != nil {
			return nil, fmt.Errorf("解析已选 Release %q 的发布时间失败：%w", release.Version, err)
		}
		previous = append(previous, release)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	incomingIDs := make(map[int64]struct{}, len(incoming))
	observedIDs := make(map[int64]struct{}, len(observed))
	candidates := append([]ResourceVersion(nil), incoming...)
	for _, release := range incoming {
		incomingIDs[release.NumericID] = struct{}{}
	}
	for _, release := range observed {
		observedIDs[release.NumericID] = struct{}{}
	}
	for _, release := range previous {
		if _, ok := incomingIDs[release.NumericID]; !ok {
			candidates = append(candidates, release)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return newerResourceVersion(candidates[i], candidates[j])
	})
	if keep > 0 && len(candidates) > keep {
		candidates = candidates[:keep]
	}
	missing := make([]ResourceVersion, 0)
	for _, release := range candidates {
		if _, ok := incomingIDs[release.NumericID]; ok {
			continue
		}
		if _, ok := observedIDs[release.NumericID]; ok {
			continue
		}
		if replacedByNewerSameVersion(release, incoming) {
			continue
		}
		missing = append(missing, release)
	}
	return missing, nil
}

func replacedByNewerSameVersion(previous ResourceVersion, incoming []ResourceVersion) bool {
	for _, release := range incoming {
		if release.Version == previous.Version && newerResourceVersion(release, previous) {
			return true
		}
	}
	return false
}
