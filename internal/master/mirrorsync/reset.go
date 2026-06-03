package mirrorsync

import "context"

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
	if _, err := tx.ExecContext(ctx, `DELETE FROM traffic_reservations WHERE authorization_id IN (`+projectAuthorizations+`)`, projectID); err != nil {
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
