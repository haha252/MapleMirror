package mirrorsync

import (
	"context"
	"database/sql"
)

func (s Store) ResetProject(ctx context.Context, projectID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists string
	if err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE id = ?`, projectID).Scan(&exists); err != nil {
		return err
	}

	projectAssets := `SELECT a.id FROM assets a
		JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ?`
	projectAuthorizations := `SELECT da.id FROM download_authorizations da
		JOIN assets a ON a.id = da.asset_id
		JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ?`

	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_events WHERE authorization_id IN (`+projectAuthorizations+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_event_dedupe WHERE authorization_id IN (`+projectAuthorizations+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_reservations WHERE authorization_id IN (`+projectAuthorizations+`)`, projectID); err != nil {
		return err
	}
	if err := subtractProjectPublicStats(ctx, tx, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM download_authorizations WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM challenges WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM daily_asset_stats WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM asset_stat_totals WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_stat_totals WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_tasks WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_inventory WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM target_inventory WHERE asset_id IN (`+projectAssets+`)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM assets WHERE release_id IN (
		SELECT id FROM releases WHERE project_id = ?)`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM releases WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sync_scans WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM project_scan_state WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM daily_project_stats WHERE project_id = ?`, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

func subtractProjectPublicStats(ctx context.Context, tx *sql.Tx, projectID string) error {
	// sent_bytes 是已经实际发生的历史网络流量，不属于可随项目派生数据重置的计数。
	if _, err := tx.ExecContext(ctx, `UPDATE daily_public_stats SET
		authorization_count = MAX(authorization_count - COALESCE((
			SELECT SUM(authorization_count) FROM daily_project_stats dps
			WHERE dps.project_id = ? AND dps.stat_day = daily_public_stats.stat_day
		), 0), 0),
		web_authorization_count = MAX(web_authorization_count - COALESCE((
			SELECT SUM(web_authorization_count) FROM daily_project_stats dps
			WHERE dps.project_id = ? AND dps.stat_day = daily_public_stats.stat_day
		), 0), 0),
		api_authorization_count = MAX(api_authorization_count - COALESCE((
			SELECT SUM(api_authorization_count) FROM daily_project_stats dps
			WHERE dps.project_id = ? AND dps.stat_day = daily_public_stats.stat_day
		), 0), 0),
		transfer_started_count = MAX(transfer_started_count - COALESCE((
			SELECT SUM(transfer_started_count) FROM daily_project_stats dps
			WHERE dps.project_id = ? AND dps.stat_day = daily_public_stats.stat_day
		), 0), 0)
		WHERE stat_day IN (
			SELECT stat_day FROM daily_project_stats WHERE project_id = ?
		)`, projectID, projectID, projectID, projectID, projectID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE public_stat_totals SET
		authorization_count = MAX(authorization_count - COALESCE((
			SELECT SUM(authorization_count) FROM daily_project_stats WHERE project_id = ?
		), 0), 0),
		web_authorization_count = MAX(web_authorization_count - COALESCE((
			SELECT SUM(web_authorization_count) FROM daily_project_stats WHERE project_id = ?
		), 0), 0),
		api_authorization_count = MAX(api_authorization_count - COALESCE((
			SELECT SUM(api_authorization_count) FROM daily_project_stats WHERE project_id = ?
		), 0), 0),
		transfer_started_count = MAX(transfer_started_count - COALESCE((
			SELECT SUM(transfer_started_count) FROM daily_project_stats WHERE project_id = ?
		), 0), 0)
		WHERE id = 'global'`, projectID, projectID, projectID, projectID)
	return err
}
