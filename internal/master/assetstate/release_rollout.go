package assetstate

import (
	"context"
	"database/sql"
	"strings"
)

// FinalizeReleaseRollout trims temporary serving-release overlap back to the
// configured retain_versions window. Only releases that still contain at least
// one mirrorable asset consume a retain slot; metadata-only/fully-filtered
// releases are deselected immediately. Older serving releases stay selected
// until every asset in the newest retained release window has a verified,
// required, online replica.
func FinalizeReleaseRollout(ctx context.Context, tx *sql.Tx, projectID string) (bool, error) {
	var keep int
	if err := tx.QueryRowContext(ctx, `SELECT retain_versions FROM projects WHERE id = ? AND enabled = 1`, projectID).Scan(&keep); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	if keep <= 0 {
		return false, nil
	}

	selected, err := selectedMirrorableReleases(ctx, tx, projectID)
	if err != nil {
		return false, err
	}
	keepIDs := selected
	if len(keepIDs) > keep {
		keepIDs = keepIDs[:keep]
		for _, releaseID := range keepIDs {
			ready, err := releaseReadyForCutover(ctx, tx, releaseID)
			if err != nil {
				return false, err
			}
			if !ready {
				return deselectEmptySelectedReleases(ctx, tx, projectID)
			}
		}
	}
	return deselectOutsideRetainWindow(ctx, tx, projectID, keepIDs)
}

func selectedMirrorableReleases(ctx context.Context, tx *sql.Tx, projectID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT r.id FROM releases r
		WHERE r.project_id = ? AND r.selected = 1
		AND EXISTS (SELECT 1 FROM assets a WHERE a.release_id = r.id
			AND a.service_state IN ('candidate','pending','active','superseded'))
		ORDER BY r.published_at DESC, r.github_release_id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func deselectEmptySelectedReleases(ctx context.Context, tx *sql.Tx, projectID string) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE releases SET selected = 0
		WHERE project_id = ? AND selected = 1
		AND NOT EXISTS (SELECT 1 FROM assets a WHERE a.release_id = releases.id
			AND a.service_state IN ('candidate','pending','active','superseded'))`, projectID)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func deselectOutsideRetainWindow(ctx context.Context, tx *sql.Tx, projectID string, keepIDs []string) (bool, error) {
	args := []any{projectID}
	query := `UPDATE releases SET selected = 0 WHERE project_id = ? AND selected = 1`
	if len(keepIDs) > 0 {
		query += ` AND id NOT IN (` + strings.TrimRight(strings.Repeat("?,", len(keepIDs)), ",") + `)`
		for _, id := range keepIDs {
			args = append(args, id)
		}
	}
	result, err := tx.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	n, _ := result.RowsAffected()
	return n > 0, nil
}

func releaseReadyForCutover(ctx context.Context, tx *sql.Tx, releaseID string) (bool, error) {
	var eligible, unavailable int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN EXISTS (
		SELECT 1 FROM node_inventory ni
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = a.id AND ti.desired_state = 'required'
		JOIN nodes n ON n.id = ni.node_id
		WHERE ni.asset_id = a.id AND ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256
			AND ni.size_bytes = a.size_bytes
			AND n.state NOT IN ('disabled', 'offline')
			AND n.last_heartbeat_at IS NOT NULL AND n.last_heartbeat_at != ''
			AND n.public_download_base_url != ''
	) THEN 0 ELSE 1 END), 0)
	FROM assets a WHERE a.release_id = ?
		AND a.service_state IN ('candidate', 'pending', 'active', 'superseded')`, releaseID).Scan(&eligible, &unavailable)
	if err != nil {
		return false, err
	}
	return eligible > 0 && unavailable == 0, nil
}
