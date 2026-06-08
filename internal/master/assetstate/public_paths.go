package assetstate

import (
	"context"
	"database/sql"
	"sort"

	"mirror-server/internal/downloadurl"
	"mirror-server/internal/master/assignment"
)

type publicPathAsset struct {
	ID              string
	PathKey         string
	State           string
	PublishedAt     string
	GitHubReleaseID int64
	GitHubAssetID   int64
}

func ReconcileAssetProject(ctx context.Context, tx *sql.Tx, assetID string) (string, error) {
	var projectID string
	err := tx.QueryRowContext(ctx, `SELECT r.project_id FROM assets a
		JOIN releases r ON r.id = a.release_id
		WHERE a.id = ? AND r.selected = 1`, assetID).Scan(&projectID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return projectID, ReconcilePublicPaths(ctx, tx, projectID)
}

func ReconcilePublicPaths(ctx context.Context, tx *sql.Tx, projectID string) error {
	assets, err := loadPathAssets(ctx, tx, projectID)
	if err != nil {
		return err
	}
	groups := map[string][]publicPathAsset{}
	for _, asset := range assets {
		groups[asset.PathKey] = append(groups[asset.PathKey], asset)
	}
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool {
			return newerAsset(group[i], group[j])
		})
		publicID, err := choosePublicAsset(ctx, tx, group)
		if err != nil {
			return err
		}
		for i, asset := range group {
			next := "superseded"
			if asset.ID == publicID {
				next = "candidate"
			} else if i == 0 {
				next = "pending"
			}
			if next == asset.State {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE assets SET service_state = ? WHERE id = ?`,
				next, asset.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func RebuildTargetInventory(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	if err := assignment.ReconcileAllNodes(ctx, tx, now); err != nil {
		return err
	}
	return assignment.RebuildProjectTargets(ctx, tx, projectID, now)
}

func CancelObsoleteDownloadTasks(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	return assignment.CancelObsoleteProjectTasks(ctx, tx, projectID, now)
}

func loadPathAssets(ctx context.Context, tx *sql.Tx, projectID string) ([]publicPathAsset, error) {
	rows, err := tx.QueryContext(ctx, `SELECT a.id, r.tag_name || '/' || a.file_name,
		a.service_state, r.published_at, r.github_release_id, a.github_asset_id
		FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND r.selected = 1
		AND a.service_state IN ('candidate', 'pending', 'superseded')`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []publicPathAsset
	for rows.Next() {
		var asset publicPathAsset
		if err := rows.Scan(&asset.ID, &asset.PathKey, &asset.State,
			&asset.PublishedAt, &asset.GitHubReleaseID, &asset.GitHubAssetID); err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, rows.Err()
}

func choosePublicAsset(ctx context.Context, tx *sql.Tx, group []publicPathAsset) (string, error) {
	if len(group) == 0 {
		return "", nil
	}
	if ok, err := hasRoutableReplica(ctx, tx, group[0].ID); ok || err != nil {
		return group[0].ID, err
	}
	for _, asset := range group[1:] {
		if ok, err := hasRoutableReplica(ctx, tx, asset.ID); ok || err != nil {
			return asset.ID, err
		}
	}
	return group[0].ID, nil
}

func hasRoutableReplica(ctx context.Context, tx *sql.Tx, assetID string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT n.public_download_base_url
		FROM node_inventory ni JOIN assets a ON a.id = ni.asset_id
			AND a.service_state IN ('candidate', 'pending')
		JOIN releases r ON r.id = a.release_id AND r.selected = 1
		JOIN projects p ON p.id = r.project_id AND p.enabled = 1
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = ni.asset_id AND ti.desired_state = 'required'
		JOIN nodes n ON n.id = ni.node_id
		WHERE ni.asset_id = ? AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
		AND n.state NOT IN ('disabled', 'offline')
		AND n.last_heartbeat_at IS NOT NULL AND n.last_heartbeat_at != ''
		AND n.public_download_base_url != ''`, assetID)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var baseURL string
		if err := rows.Scan(&baseURL); err != nil {
			return false, err
		}
		if _, ok := downloadurl.NormalizeBase(baseURL); ok {
			return true, nil
		}
	}
	return false, rows.Err()
}

func newerAsset(a, b publicPathAsset) bool {
	if a.PublishedAt != b.PublishedAt {
		return a.PublishedAt > b.PublishedAt
	}
	if a.GitHubReleaseID != b.GitHubReleaseID {
		return a.GitHubReleaseID > b.GitHubReleaseID
	}
	return a.GitHubAssetID > b.GitHubAssetID
}
