package mirrorsync

import (
	"context"
	"fmt"
	"sort"
	"time"
)

func (s Store) MissingSelectedReleases(ctx context.Context, projectID string,
	incoming []ResourceVersion, keep int) ([]ResourceVersion, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT github_release_id, tag_name, published_at
		FROM releases WHERE project_id = ? AND selected = 1
		ORDER BY published_at DESC, github_release_id DESC`, projectID)
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
	candidates := append([]ResourceVersion(nil), incoming...)
	for _, release := range incoming {
		incomingIDs[release.NumericID] = struct{}{}
	}
	for _, release := range previous {
		if _, ok := incomingIDs[release.NumericID]; !ok {
			candidates = append(candidates, release)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].PublishedAt.Equal(candidates[j].PublishedAt) {
			return candidates[i].NumericID > candidates[j].NumericID
		}
		return candidates[i].PublishedAt.After(candidates[j].PublishedAt)
	})
	if keep > 0 && len(candidates) > keep {
		candidates = candidates[:keep]
	}
	missing := make([]ResourceVersion, 0)
	for _, release := range candidates {
		if _, ok := incomingIDs[release.NumericID]; !ok {
			missing = append(missing, release)
		}
	}
	return missing, nil
}
