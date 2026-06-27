package control

import (
	"context"
	"database/sql"
	"log/slog"
)

type inventoryAcceptResult struct {
	AssetID        string
	State          string
	PublicAsset    bool
	TargetRequired bool
	ExpectedDigest string
	ExpectedSize   int64
	LocalDigest    string
	LocalSize      int64
}

func publicCandidateAsset(ctx context.Context, tx interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, assetID string) bool {
	var exists int
	_ = tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM assets a
		JOIN releases r ON r.id = a.release_id AND r.selected = 1
		JOIN projects p ON p.id = r.project_id AND p.enabled = 1
		WHERE a.id = ? AND a.service_state = 'candidate'
	)`, assetID).Scan(&exists)
	return exists == 1
}

func (r Repository) quarantineNodeForPublicAssetMismatch(ctx context.Context, tx *sql.Tx,
	session Session, result inventoryAcceptResult, now string) error {
	var currentState string
	err := tx.QueryRowContext(ctx, `SELECT state FROM nodes WHERE id = ?`, session.NodeID).Scan(&currentState)
	if err != nil {
		return err
	}
	if currentState == "disabled" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE nodes SET state = 'disabled',
		routing_ready = 0, updated_at = ? WHERE id = ?`, now, session.NodeID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE node_certificates SET status = 'revoked',
		revoked_at = ? WHERE node_id = ? AND status = 'active'`, now, session.NodeID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE node_control_sessions SET disconnected_at = ?,
		close_reason = '公开资产库存校验不一致' WHERE node_id = ? AND disconnected_at IS NULL`,
		now, session.NodeID); err != nil {
		return err
	}
	if err := auditTx(ctx, tx, "node.security_quarantine", "node", session.NodeID, "success",
		session.RequestID, "公开资产库存校验不一致，节点已隔离并撤销证书", ""); err != nil {
		return err
	}
	if r.Logger != nil {
		r.Logger.Error(ctx, "公开资产库存校验不一致，节点已隔离",
			slog.String("node_id", session.NodeID),
			slog.String("asset_id", result.AssetID),
			slog.String("request_id", session.RequestID),
			slog.String("local_digest_sha256", result.LocalDigest),
			slog.String("expected_digest_sha256", result.ExpectedDigest),
			slog.Int64("local_size_bytes", result.LocalSize),
			slog.Int64("expected_size_bytes", result.ExpectedSize))
	}
	return nil
}
