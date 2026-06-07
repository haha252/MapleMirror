package assignment

import (
	"context"
	"database/sql"
	"time"
)

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func ReconcileAllNodes(ctx context.Context, tx *sql.Tx, now string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM nodes WHERE state != 'disabled'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := ReconcileNode(ctx, tx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func ReconcileNode(ctx context.Context, tx *sql.Tx, nodeID, now string) error {
	projects, assigned, mode, err := calculateAssignments(ctx, tx, nodeID, now)
	if err != nil {
		return err
	}
	if err := persistAssignments(ctx, tx, nodeID, mode, projects, assigned, now); err != nil {
		return err
	}
	if err := rebuildNodeTargets(ctx, tx, nodeID, now); err != nil {
		return err
	}
	return CancelObsoleteNodeTasks(ctx, tx, nodeID, now)
}

func calculateAssignments(ctx context.Context, tx *sql.Tx, nodeID, now string) ([]projectScore, map[string]bool, string, error) {
	max, mode, err := nodeAssignmentConfig(ctx, tx, nodeID)
	if err != nil {
		return nil, nil, "", err
	}
	if mode == ModeManual {
		projects, err := loadProjectScores(ctx, tx, nodeID, false)
		assigned := manualAssigned(projects)
		if max > 0 {
			trimAssigned(projects, assigned, max)
		}
		return projects, assigned, mode, err
	}
	projects, err := loadProjectScores(ctx, tx, nodeID, true)
	if err != nil {
		return nil, nil, "", err
	}
	if max == 0 {
		return projects, assignAll(projects), ModeAuto, nil
	}
	return projects, autoAssigned(projects, max, parseTime(now)), ModeAuto, nil
}

func autoAssigned(projects []projectScore, max int, now time.Time) map[string]bool {
	current := currentAssigned(projects)
	if len(current) == 0 {
		return takeTop(projects, max)
	}
	assigned := map[string]bool{}
	for _, p := range current {
		assigned[p.id] = true
	}
	trimAssigned(projects, assigned, max)
	fillAssigned(projects, assigned, max)
	replaceAssigned(projects, assigned, now)
	return assigned
}

func replaceAssigned(projects []projectScore, assigned map[string]bool, now time.Time) {
	for _, candidate := range projects {
		if assigned[candidate.id] {
			continue
		}
		worst, ok := worstReplaceable(projects, assigned, now)
		if !ok || !beats(candidate.score, worst.score) {
			return
		}
		delete(assigned, worst.id)
		assigned[candidate.id] = true
	}
}

func worstReplaceable(projects []projectScore, assigned map[string]bool, now time.Time) (projectScore, bool) {
	var worst projectScore
	ok := false
	for _, p := range projects {
		if !assigned[p.id] || protected(p, now) {
			continue
		}
		if !ok || p.score < worst.score || (p.score == worst.score && p.id > worst.id) {
			worst, ok = p, true
		}
	}
	return worst, ok
}

func protected(p projectScore, now time.Time) bool {
	changed, err := time.Parse(time.RFC3339Nano, p.lastChangedAt)
	return err == nil && now.Sub(changed) < minRetention
}

func beats(candidate, current int64) bool {
	if current <= 0 {
		return candidate > current
	}
	return float64(candidate) >= float64(current)*replacementThreshold
}
