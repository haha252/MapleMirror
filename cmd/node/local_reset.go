package main

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"

	"mirror-server/internal/config"
	"mirror-server/internal/logging"
)

func resetNodeLocalData(cfg config.Node, db *sql.DB, logger *logging.Logger) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	for _, statement := range []string{
		`DELETE FROM pending_sync_task_results`,
		`DELETE FROM local_sync_tasks`,
		`DELETE FROM pending_traffic_events`,
		`DELETE FROM local_assets`,
		`DELETE FROM inventory_report_cursor`,
		`DELETE FROM control_state`,
		`DELETE FROM node_enrollment_state`,
		`DELETE FROM control_identity`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	removed := 0
	for _, path := range []string{
		cfg.Storage.Directory,
		cfg.Storage.TempDirectory,
		cfg.TLS.CAFile,
		cfg.TLS.CertFile,
		cfg.TLS.KeyFile,
		cfg.Pairing.CodeFile,
		cfg.Pairing.CredentialFile,
		cfg.Download.VerifyPublicKeyFile,
	} {
		if removeConfiguredPath(path) == nil {
			removed++
		}
	}
	if logger != nil {
		logger.Info(context.Background(), "节点重新配置已清理旧本地数据",
			slog.Int("removed_paths", removed))
	}
	return nil
}

func removeConfiguredPath(path string) error {
	clean := filepath.Clean(path)
	if clean == "" || clean == "." || clean == string(os.PathSeparator) {
		return nil
	}
	if err := os.RemoveAll(clean); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// resetNodeIdentityForReEnrollment removes credentials and control/accounting
// state that is bound to the old identity, but deliberately preserves mirrored
// asset files, local_assets and managed Swarm partials.
func resetNodeIdentityForReEnrollment(cfg config.Node, db *sql.DB, logger *logging.Logger) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		`DELETE FROM pending_sync_task_results`,
		`DELETE FROM local_sync_tasks`,
		`DELETE FROM pending_traffic_events`,
		`DELETE FROM local_authorizations`,
		`DELETE FROM inventory_report_cursor`,
		`DELETE FROM control_state`,
		`DELETE FROM node_enrollment_state`,
		`DELETE FROM control_identity`,
	} {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	removed := 0
	for _, path := range []string{
		cfg.TLS.CAFile, cfg.TLS.CertFile, cfg.TLS.KeyFile,
		cfg.Pairing.CodeFile, cfg.Pairing.CredentialFile,
		cfg.Download.VerifyPublicKeyFile,
	} {
		if removeConfiguredPath(path) == nil {
			removed++
		}
	}
	if logger != nil {
		logger.Info(context.Background(), "节点重新登记已重置身份状态并保留镜像资产",
			slog.Int("removed_identity_paths", removed),
			slog.String("asset_directory", cfg.Storage.Directory))
	}
	return nil
}
