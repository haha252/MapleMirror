package assignment

import (
	"context"
	"database/sql"
	"time"
)

type rowScanner interface {
	Scan(...any) error
}

func nodeAssignmentConfig(ctx context.Context, q queryer, nodeID string) (int, string, error) {
	var max int
	var mode string
	err := q.QueryRowContext(ctx, `SELECT max_mirror_projects,
		project_assignment_mode FROM nodes WHERE id = ?`, nodeID).Scan(&max, &mode)
	if mode != ModeManual {
		mode = ModeAuto
	}
	return max, mode, err
}

func loadProjectScores(ctx context.Context, tx *sql.Tx, nodeID string, onlyMirrorable bool) ([]projectScore, error) {
	rows, err := tx.QueryContext(ctx, projectScoreSQL(onlyMirrorable), heatStart(), heatEnd(), nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []projectScore
	for rows.Next() {
		var item projectScore
		if err := scanProjectScore(rows, &item); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func projectScoreSQL(onlyMirrorable bool) string {
	where := "WHERE p.enabled = 1"
	if onlyMirrorable {
		where += ` AND EXISTS (
			SELECT 1 FROM releases r JOIN assets a ON a.release_id = r.id
			WHERE r.project_id = p.id AND r.selected = 1
			AND a.service_state IN ('candidate', 'pending'))`
	}
	return `SELECT p.id, p.name, COALESCE(SUM(dps.authorization_count), 0),
		COALESCE(npa.assigned, 0), COALESCE(npa.pinned, 0),
		COALESCE(npa.last_changed_at, '')
		FROM projects p
		LEFT JOIN daily_project_stats dps ON dps.project_id = p.id
			AND dps.stat_day BETWEEN ? AND ?
		LEFT JOIN node_project_assignments npa ON npa.project_id = p.id
			AND npa.node_id = ? ` + where + `
		GROUP BY p.id, p.name, npa.assigned, npa.pinned, npa.last_changed_at
		ORDER BY COALESCE(SUM(dps.authorization_count), 0) DESC, p.name, p.id`
}

func scanProjectScore(row rowScanner, item *projectScore) error {
	var assigned, pinned int
	err := row.Scan(&item.id, &item.name, &item.score, &assigned, &pinned, &item.lastChangedAt)
	item.assigned = assigned == 1
	item.pinned = pinned == 1
	return err
}

func heatStart() string {
	return time.Now().UTC().AddDate(0, 0, 1-heatWindowDays).Format("2006-01-02")
}

func heatEnd() string {
	return time.Now().UTC().Format("2006-01-02")
}

func parseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Now().UTC()
	}
	return parsed
}
