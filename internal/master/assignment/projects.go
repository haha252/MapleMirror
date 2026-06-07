package assignment

import (
	"context"
	"database/sql"
	"time"
)

func ReadNodeProjects(ctx context.Context, db *sql.DB, nodeID string) (NodeProjects, error) {
	var out NodeProjects
	if err := db.QueryRowContext(ctx, `SELECT id, project_assignment_mode,
		max_mirror_projects FROM nodes WHERE id = ?`, nodeID).
		Scan(&out.NodeID, &out.AssignmentMode, &out.MaxMirrorProjects); err != nil {
		return out, err
	}
	rows, err := db.QueryContext(ctx, projectScoreSQL(false), heatStart(), heatEnd(), nodeID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item projectScore
		if err := scanProjectScore(rows, &item); err != nil {
			return out, err
		}
		out.Projects = append(out.Projects, Project{
			ID: item.id, Name: item.name, Score: item.score,
			Assigned: item.assigned, Pinned: item.pinned,
			LastChangedAt: item.lastChangedAt,
		})
	}
	return out, rows.Err()
}

func SaveNodeProjects(ctx context.Context, db *sql.DB, nodeID, mode string, selected []string, now time.Time) error {
	if mode != ModeManual {
		mode = ModeAuto
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	max, err := nodeLimit(ctx, tx, nodeID)
	if err != nil {
		return err
	}
	if mode == ModeManual && max > 0 && len(unique(selected)) > max {
		return ErrManualLimitExceeded
	}
	nowText := now.UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, `UPDATE nodes SET project_assignment_mode = ?,
		updated_at = ? WHERE id = ?`, mode, nowText, nodeID); err != nil {
		return err
	}
	if mode == ModeManual {
		if err := saveManual(ctx, tx, nodeID, unique(selected), nowText); err != nil {
			return err
		}
	} else if err := ReconcileNode(ctx, tx, nodeID, nowText); err != nil {
		return err
	} else if _, err := GenerateNodeTasks(ctx, tx, nodeID, nowText); err != nil {
		return err
	}
	if mode == ModeManual {
		if err := rebuildNodeTargets(ctx, tx, nodeID, nowText); err != nil {
			return err
		}
		if err := CancelObsoleteNodeTasks(ctx, tx, nodeID, nowText); err != nil {
			return err
		}
		if _, err := GenerateNodeTasks(ctx, tx, nodeID, nowText); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nodeLimit(ctx context.Context, q queryer, nodeID string) (int, error) {
	var max int
	err := q.QueryRowContext(ctx, `SELECT max_mirror_projects FROM nodes WHERE id = ?`, nodeID).Scan(&max)
	return max, err
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
