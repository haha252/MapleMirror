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

const syncScanRetainPerProject = 50

type ScanSummary struct {
	ScanID              string `json:"scan_id"`
	ProjectID           string `json:"project_id,omitempty"`
	State               string `json:"state"`
	SelectedReleases    int    `json:"selected_releases"`
	AcceptedAssets      int    `json:"accepted_assets"`
	RejectedAssets      int    `json:"rejected_assets"`
	RequestID           string `json:"request_id"`
	StartedAt           string `json:"started_at,omitempty"`
	CompletedAt         string `json:"completed_at,omitempty"`
	NextScanAt          string `json:"next_scan_at,omitempty"`
	LastScanStartedAt   string `json:"last_scan_started_at,omitempty"`
	LastScanCompletedAt string `json:"last_scan_completed_at,omitempty"`
	LastErrorMessage    string `json:"last_error_message,omitempty"`
}

type SyncStatus struct {
	NodeID                  string `json:"node_id"`
	ConnectionState         string `json:"connection_state"`
	SyncPhase               string `json:"sync_phase"`
	RoutingReady            bool   `json:"routing_ready"`
	RequiredAssets          int    `json:"required_assets"`
	VerifiedAssets          int    `json:"verified_assets"`
	MissingAssets           int    `json:"missing_assets"`
	MismatchedAssets        int    `json:"mismatched_assets"`
	PendingTasks            int    `json:"pending_tasks"`
	SentTasks               int    `json:"sent_tasks"`
	RunningTasks            int    `json:"running_tasks"`
	RetryWaitTasks          int    `json:"retry_wait_tasks"`
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
	started := nowText()
	_, err = s.DB.ExecContext(ctx, `INSERT INTO sync_scans
		(id, project_id, state, request_id, started_at)
		VALUES (?, ?, 'running', ?, ?)`,
		id, nullable(projectID), requestID, started)
	if err == nil && projectID != "" {
		err = s.MarkProjectScanStarted(ctx, projectID, id, started)
	}
	return id, err
}

func (s Store) FinishScan(ctx context.Context, scanID string, summary ScanSummary, errText string) error {
	state := "succeeded"
	if errText != "" {
		state = "failed"
	}
	completed := nowText()
	_, err := s.DB.ExecContext(ctx, `UPDATE sync_scans SET state = ?,
		selected_releases = ?, accepted_assets = ?, rejected_assets = ?,
		error_message = ?, completed_at = ? WHERE id = ?`,
		state, summary.SelectedReleases, summary.AcceptedAssets,
		summary.RejectedAssets, nullable(errText), completed, scanID)
	if err == nil && summary.ProjectID != "" {
		err = s.MarkProjectScanFinished(ctx, summary.ProjectID, scanID, state, completed, errText)
	}
	if err == nil {
		err = s.PruneScanHistory(ctx, summary.ProjectID, syncScanRetainPerProject)
	}
	return err
}

func (s Store) PruneScanHistory(ctx context.Context, projectID string, retain int) error {
	if retain <= 0 {
		retain = syncScanRetainPerProject
	}
	key := projectID
	_, err := s.DB.ExecContext(ctx, `DELETE FROM sync_scans
		WHERE COALESCE(project_id, '') = ? AND state != 'running'
		AND id NOT IN (
			SELECT id FROM sync_scans
			WHERE COALESCE(project_id, '') = ? AND state != 'running'
			ORDER BY started_at DESC, id DESC LIMIT ?
		)`, key, key, retain)
	return err
}

func (s Store) LatestScan(ctx context.Context, projectID string) (ScanSummary, error) {
	query := `SELECT sc.id, COALESCE(sc.project_id, ''), sc.state,
		sc.selected_releases, sc.accepted_assets, sc.rejected_assets,
		sc.request_id, sc.started_at, COALESCE(sc.completed_at, ''),
		COALESCE(ps.next_scan_at, ''), COALESCE(ps.last_scan_started_at, ''),
		COALESCE(ps.last_scan_completed_at, ''), COALESCE(ps.last_error_message, '')
		FROM sync_scans sc LEFT JOIN project_scan_state ps
		ON ps.project_id = sc.project_id`
	args := []any{}
	if projectID != "" {
		query += ` WHERE sc.project_id = ?`
		args = append(args, projectID)
	}
	query += ` ORDER BY sc.started_at DESC LIMIT 1`
	var out ScanSummary
	err := s.DB.QueryRowContext(ctx, query, args...).Scan(&out.ScanID, &out.ProjectID,
		&out.State, &out.SelectedReleases, &out.AcceptedAssets,
		&out.RejectedAssets, &out.RequestID, &out.StartedAt, &out.CompletedAt,
		&out.NextScanAt, &out.LastScanStartedAt, &out.LastScanCompletedAt,
		&out.LastErrorMessage)
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
		`SELECT state, routing_ready, COALESCE(last_heartbeat_at, '') FROM nodes WHERE id = ?`, nodeID).
		Scan(&out.ConnectionState, &ready, &out.LastHeartbeatAt); err != nil {
		return out, err
	}
	out.RoutingReady = ready == 1
	out.RequiredAssets, out.MissingAssets = targetInventoryCounts(ctx, s.DB, nodeID)
	out.VerifiedAssets, out.MismatchedAssets = nodeInventoryCounts(ctx, s.DB, nodeID)
	taskCounts := syncTaskCounts(ctx, s.DB, nodeID)
	out.PendingTasks = taskCounts["pending"]
	out.SentTasks = taskCounts["sent"]
	out.RunningTasks = taskCounts["running"]
	out.RetryWaitTasks = taskCounts["retry_wait"]
	out.FailedTasks = taskCounts["failed"]
	out.OutstandingTasks = out.PendingTasks + out.SentTasks + out.RunningTasks +
		out.RetryWaitTasks + out.FailedTasks
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
	out.SyncPhase = syncPhase(out)
	out.RoutingReadyReason, out.RoutingReadyDetail = syncStatusReason(out)
	return out, nil
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
