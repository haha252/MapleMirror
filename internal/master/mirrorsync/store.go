package mirrorsync

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	mastercontrol "mirror-server/internal/master/control"
	"mirror-server/internal/protocol"
)

type Store struct {
	DB      *sql.DB
	Runtime *mastercontrol.RuntimeStore
}

type ScanSummary struct {
	ScanID           string `json:"scan_id"`
	ProjectID        string `json:"project_id,omitempty"`
	State            string `json:"state"`
	SelectedReleases int    `json:"selected_releases"`
	AcceptedAssets   int    `json:"accepted_assets"`
	RejectedAssets   int    `json:"rejected_assets"`
	RequestID        string `json:"request_id"`
}

type SyncStatus struct {
	NodeID                  string `json:"node_id"`
	RoutingReady            bool   `json:"routing_ready"`
	RequiredAssets          int    `json:"required_assets"`
	VerifiedAssets          int    `json:"verified_assets"`
	MissingAssets           int    `json:"missing_assets"`
	MismatchedAssets        int    `json:"mismatched_assets"`
	RunningTasks            int    `json:"running_tasks"`
	FailedTasks             int    `json:"failed_tasks"`
	LatestInventoryRevision int    `json:"latest_inventory_revision"`
	LatestInventoryComplete bool   `json:"latest_inventory_complete"`
	HasInventoryReport      bool   `json:"has_inventory_report"`
	ActiveControlSession    bool   `json:"active_control_session"`
	LastHeartbeatAt         string `json:"last_heartbeat_at,omitempty"`
	RoutingReadyReason      string `json:"routing_ready_reason"`
	RoutingReadyDetail      string `json:"routing_ready_detail"`
	OutstandingTasks        int    `json:"-"`
}

func (s Store) StartScan(ctx context.Context, projectID, requestID string) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO sync_scans
		(id, project_id, state, request_id, started_at)
		VALUES (?, ?, 'running', ?, ?)`,
		id, nullable(projectID), requestID, nowText())
	return id, err
}

func (s Store) FinishScan(ctx context.Context, scanID string, summary ScanSummary, errText string) error {
	state := "succeeded"
	if errText != "" {
		state = "failed"
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE sync_scans SET state = ?,
		selected_releases = ?, accepted_assets = ?, rejected_assets = ?,
		error_message = ?, completed_at = ? WHERE id = ?`,
		state, summary.SelectedReleases, summary.AcceptedAssets,
		summary.RejectedAssets, nullable(errText), nowText(), scanID)
	return err
}

func (s Store) LatestScan(ctx context.Context, projectID string) (ScanSummary, error) {
	query := `SELECT id, COALESCE(project_id, ''), state, selected_releases,
		accepted_assets, rejected_assets, request_id FROM sync_scans`
	args := []any{}
	if projectID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY started_at DESC LIMIT 1`
	var out ScanSummary
	err := s.DB.QueryRowContext(ctx, query, args...).Scan(&out.ScanID, &out.ProjectID,
		&out.State, &out.SelectedReleases, &out.AcceptedAssets,
		&out.RejectedAssets, &out.RequestID)
	return out, err
}

func (s Store) NextTask(ctx context.Context, nodeID string) (protocol.SyncTask, bool, error) {
	var task protocol.SyncTask
	err := s.DB.QueryRowContext(ctx, `SELECT t.id, t.task_type, a.id,
		r.project_id, r.tag_name, a.file_name, a.size_bytes, a.source_url, a.digest_sha256
		FROM node_tasks t LEFT JOIN assets a ON a.id = t.asset_id
		LEFT JOIN releases r ON r.id = a.release_id
		WHERE t.node_id = ?
		AND (
			t.state = 'pending'
			OR (t.state = 'retry_wait' AND (t.retry_after IS NULL OR t.retry_after = '' OR t.retry_after <= ?))
		)
		ORDER BY t.created_at LIMIT 1`, nodeID, nowText()).
		Scan(&task.TaskID, &task.TaskType, &task.Asset.AssetID, &task.Asset.ProjectID,
			&task.Asset.Version, &task.Asset.FileName,
			&task.Asset.SizeBytes, &task.Asset.DownloadURL, &task.Asset.DigestSHA256)
	if err == sql.ErrNoRows {
		return protocol.SyncTask{}, false, nil
	}
	if err != nil {
		return protocol.SyncTask{}, false, err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'sent',
		updated_at = ? WHERE id = ?`, nowText(), task.TaskID)
	return task, true, err
}

func (s Store) SyncStatus(ctx context.Context, nodeID string) (SyncStatus, error) {
	out := SyncStatus{NodeID: nodeID}
	var ready int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT routing_ready, COALESCE(last_heartbeat_at, '') FROM nodes WHERE id = ?`, nodeID).
		Scan(&ready, &out.LastHeartbeatAt); err != nil {
		return out, err
	}
	out.RoutingReady = ready == 1
	out.RequiredAssets = count(ctx, s.DB, `SELECT COUNT(*) FROM target_inventory
		WHERE node_id = ? AND desired_state = 'required'`, nodeID)
	out.VerifiedAssets = count(ctx, s.DB, `SELECT COUNT(*) FROM node_inventory
		WHERE node_id = ? AND state = 'verified'`, nodeID)
	out.MissingAssets = count(ctx, s.DB, `SELECT COUNT(*) FROM target_inventory ti
		LEFT JOIN node_inventory ni ON ni.node_id = ti.node_id AND ni.asset_id = ti.asset_id
		WHERE ti.node_id = ? AND ti.desired_state = 'required'
		AND (ni.asset_id IS NULL OR ni.state != 'verified')`, nodeID)
	out.MismatchedAssets = count(ctx, s.DB, `SELECT COUNT(*) FROM node_inventory
		WHERE node_id = ? AND state = 'mismatch'`, nodeID)
	out.RunningTasks = count(ctx, s.DB, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('sent', 'running')`, nodeID)
	out.OutstandingTasks = count(ctx, s.DB, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state IN ('pending', 'sent', 'running', 'retry_wait', 'failed')`, nodeID)
	out.FailedTasks = count(ctx, s.DB, `SELECT COUNT(*) FROM node_tasks
		WHERE node_id = ? AND state = 'failed'`, nodeID)
	if s.Runtime != nil {
		out.LatestInventoryRevision, out.LatestInventoryComplete, out.HasInventoryReport =
			s.Runtime.LatestInventoryState(nodeID)
	} else {
		var latestComplete int
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(revision, 0), COALESCE(complete, 0)
			FROM node_inventory_reports WHERE node_id = ?
			ORDER BY reported_at DESC LIMIT 1`, nodeID).
			Scan(&out.LatestInventoryRevision, &latestComplete)
		out.LatestInventoryComplete = latestComplete == 1
		out.HasInventoryReport = out.LatestInventoryRevision > 0
	}
	if s.Runtime != nil {
		out.ActiveControlSession = s.Runtime.ActiveSession(nodeID)
	} else {
		out.ActiveControlSession = exists(ctx, s.DB, `SELECT 1 FROM node_control_sessions
			WHERE node_id = ? AND disconnected_at IS NULL`, nodeID)
	}
	out.RoutingReadyReason, out.RoutingReadyDetail = syncStatusReason(out)
	return out, nil
}

func (s Store) RetryTask(ctx context.Context, nodeID, taskID string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE node_tasks SET state = 'pending',
		error_message = NULL, attempts = 0, retry_after = NULL, updated_at = ?
		WHERE id = ? AND node_id = ?`, nowText(), taskID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s Store) CancelTask(ctx context.Context, nodeID, taskID string) error {
	return updateTask(ctx, s.DB, nodeID, taskID, "cancelled", "管理员取消")
}

func updateTask(ctx context.Context, db *sql.DB, nodeID, taskID, state, msg string) error {
	result, err := db.ExecContext(ctx, `UPDATE node_tasks SET state = ?,
		error_message = ?, updated_at = ? WHERE id = ? AND node_id = ?`,
		state, nullable(msg), nowText(), taskID, nodeID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func count(ctx context.Context, db *sql.DB, query string, arg any) int {
	var n int
	_ = db.QueryRowContext(ctx, query, arg).Scan(&n)
	return n
}

func exists(ctx context.Context, db *sql.DB, query string, arg any) bool {
	var ok int
	_ = db.QueryRowContext(ctx, `SELECT EXISTS(`+query+`)`, arg).Scan(&ok)
	return ok == 1
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nowText() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func wrap(err error, msg string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s：%w", msg, err)
}
