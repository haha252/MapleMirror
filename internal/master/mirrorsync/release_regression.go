package mirrorsync

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s Store) ReleaseRegression(ctx context.Context, projectID string, incoming []ResourceVersion) (*ResourceVersion, error) {
	var previous ResourceVersion
	var published string
	err := s.DB.QueryRowContext(ctx, `SELECT github_release_id, tag_name, published_at
		FROM releases WHERE project_id = ? AND selected = 1
		ORDER BY published_at DESC, github_release_id DESC LIMIT 1`, projectID).
		Scan(&previous.NumericID, &previous.Version, &published)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	previous.PublishedAt, err = time.Parse(time.RFC3339Nano, published)
	if err != nil {
		return nil, fmt.Errorf("解析已选 Release %q 的发布时间失败：%w", previous.Version, err)
	}
	if len(incoming) == 0 {
		return &previous, nil
	}
	current := incoming[0]
	if current.NumericID == previous.NumericID {
		return nil, nil
	}
	if current.PublishedAt.Before(previous.PublishedAt) ||
		(current.PublishedAt.Equal(previous.PublishedAt) && current.NumericID < previous.NumericID) {
		return &previous, nil
	}
	return nil, nil
}
