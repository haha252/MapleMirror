package mirrorsync

import "context"

type ProjectScanState struct {
	ProjectID           string `json:"project_id"`
	Enabled             bool   `json:"enabled"`
	LastScanStartedAt   string `json:"last_scan_started_at,omitempty"`
	LastScanCompletedAt string `json:"last_scan_completed_at,omitempty"`
	LastScanID          string `json:"last_scan_id,omitempty"`
	LastScanState       string `json:"last_scan_state,omitempty"`
	NextScanAt          string `json:"next_scan_at,omitempty"`
	LastErrorMessage    string `json:"last_error_message,omitempty"`
	UpdatedAt           string `json:"updated_at"`
}

func (s Store) DueProjects(ctx context.Context, now string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id
		FROM project_scan_state
		WHERE enabled = 1 AND (next_scan_at IS NULL OR next_scan_at = '' OR next_scan_at <= ?)
		ORDER BY COALESCE(next_scan_at, ''), project_id`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s Store) ListProjectScanStates(ctx context.Context) ([]ProjectScanState, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id, enabled,
		COALESCE(last_scan_started_at, ''), COALESCE(last_scan_completed_at, ''),
		COALESCE(last_scan_id, ''), COALESCE(last_scan_state, ''),
		COALESCE(next_scan_at, ''), COALESCE(last_error_message, ''), updated_at
		FROM project_scan_state ORDER BY project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []ProjectScanState
	for rows.Next() {
		item, err := scanProjectScanState(rows)
		if err != nil {
			return nil, err
		}
		states = append(states, item)
	}
	return states, rows.Err()
}

func scanProjectScanState(rows interface {
	Scan(...any) error
}) (ProjectScanState, error) {
	var item ProjectScanState
	var enabled int
	err := rows.Scan(&item.ProjectID, &enabled, &item.LastScanStartedAt,
		&item.LastScanCompletedAt, &item.LastScanID, &item.LastScanState,
		&item.NextScanAt, &item.LastErrorMessage, &item.UpdatedAt)
	item.Enabled = enabled == 1
	return item, err
}

func (s Store) MarkProjectScanStarted(ctx context.Context, projectID, scanID, started string) error {
	result, err := s.DB.ExecContext(ctx, `UPDATE project_scan_state SET
		last_scan_started_at = ?, last_scan_id = ?, last_scan_state = 'running',
		last_error_message = NULL, updated_at = ?
		WHERE project_id = ?`, started, scanID, started, projectID)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO project_scan_state
		(project_id, enabled, last_scan_started_at, last_scan_id,
		last_scan_state, updated_at)
		VALUES (?, 1, ?, ?, 'running', ?)`, projectID, started, scanID, started)
	return err
}

func (s Store) MarkProjectScanFinished(ctx context.Context, projectID, scanID, state, completed, errText string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE project_scan_state SET
		last_scan_completed_at = ?, last_scan_id = ?, last_scan_state = ?,
		last_error_message = ?, updated_at = ?
		WHERE project_id = ?`, completed, scanID, state, nullable(errText), completed, projectID)
	return err
}

func (s Store) SetProjectNextScan(ctx context.Context, projectID, next string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE project_scan_state SET
		next_scan_at = ?, updated_at = ? WHERE project_id = ?`, next, nowText(), projectID)
	return err
}
