package assetstate

import (
	"context"
	"database/sql"
	"sort"

	"mirror-server/internal/downloadurl"
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
	_, err := tx.ExecContext(ctx, `INSERT INTO target_inventory
		(node_id, asset_id, desired_state, updated_at)
		SELECT n.id, a.id, 'required', ?
		FROM nodes n JOIN releases r ON r.project_id = ?
		JOIN assets a ON a.release_id = r.id
		WHERE n.state != 'disabled' AND r.selected = 1
		AND a.service_state IN ('candidate', 'pending')
		ON CONFLICT(node_id, asset_id) DO UPDATE SET
		desired_state = 'required', updated_at = excluded.updated_at`,
		now, projectID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE asset_id IN (
		SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ? AND (r.selected = 0
			OR a.service_state NOT IN ('candidate', 'pending')))`,
		now, projectID)
	return err
}

func CancelObsoleteDownloadTasks(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE node_tasks SET state = 'obsolete',
		error_message = '资产已不在当前目标库存中', completed_at = ?,
		updated_at = ?, lease_expires_at = NULL
		WHERE task_type = 'asset_download'
		AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')
		AND asset_id IN (
			SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
			LEFT JOIN target_inventory ti ON ti.node_id = node_tasks.node_id
				AND ti.asset_id = a.id
			WHERE r.project_id = ?
			AND (a.service_state NOT IN ('candidate', 'pending')
				OR ti.asset_id IS NULL OR ti.desired_state != 'required')
		)`, now, now, projectID)
	return err
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
