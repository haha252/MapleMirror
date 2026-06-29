package assignment

import (
	"context"
	"database/sql"
)

func persistAssignments(ctx context.Context, tx *sql.Tx, nodeID, mode string, projects []projectScore, assigned map[string]bool, now string) error {
	for _, p := range projects {
		next := assigned[p.id]
		changed := p.lastChangedAt
		if changed == "" || p.assigned != next {
			changed = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_project_assignments
			(node_id, project_id, mode, assigned, score, pinned, last_changed_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(node_id, project_id) DO UPDATE SET
				mode = excluded.mode, assigned = excluded.assigned,
				score = excluded.score, pinned = excluded.pinned,
				last_changed_at = excluded.last_changed_at,
				updated_at = excluded.updated_at
				WHERE node_project_assignments.mode != excluded.mode
				OR node_project_assignments.assigned != excluded.assigned
				OR node_project_assignments.score != excluded.score
				OR node_project_assignments.pinned != excluded.pinned
				OR node_project_assignments.last_changed_at != excluded.last_changed_at`,
			nodeID, p.id, mode, boolInt(next), p.score, boolInt(p.pinned), changed, now); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE node_project_assignments
			SET assigned = 0, mode = ?, updated_at = ? WHERE node_id = ?
			AND project_id NOT IN (SELECT id FROM projects WHERE enabled = 1)
			AND (assigned != 0 OR mode != ?)`,
		mode, now, nodeID, mode)
	return err
}

func saveManual(ctx context.Context, tx *sql.Tx, nodeID string, selected []string, now string) error {
	selectedSet := map[string]bool{}
	for _, id := range selected {
		selectedSet[id] = true
	}
	projects, err := loadProjectScores(ctx, tx, nodeID, false)
	if err != nil {
		return err
	}
	return persistAssignments(ctx, tx, nodeID, ModeManual, projects, selectedSet, now)
}

func assignAll(projects []projectScore) map[string]bool {
	assigned := map[string]bool{}
	for _, p := range projects {
		assigned[p.id] = true
	}
	return assigned
}

func manualAssigned(projects []projectScore) map[string]bool {
	assigned := map[string]bool{}
	for _, p := range projects {
		if p.assigned {
			assigned[p.id] = true
		}
	}
	return assigned
}

func currentAssigned(projects []projectScore) []projectScore {
	var out []projectScore
	for _, p := range projects {
		if p.assigned {
			out = append(out, p)
		}
	}
	return out
}

func takeTop(projects []projectScore, max int) map[string]bool {
	assigned := map[string]bool{}
	for i, p := range projects {
		if i >= max {
			break
		}
		assigned[p.id] = true
	}
	return assigned
}

func trimAssigned(projects []projectScore, assigned map[string]bool, max int) {
	for countAssigned(assigned) > max {
		worst := ""
		for i := len(projects) - 1; i >= 0; i-- {
			if assigned[projects[i].id] {
				worst = projects[i].id
				break
			}
		}
		delete(assigned, worst)
	}
}

func fillAssigned(projects []projectScore, assigned map[string]bool, max int) {
	for _, p := range projects {
		if countAssigned(assigned) >= max {
			return
		}
		assigned[p.id] = true
	}
}

func countAssigned(assigned map[string]bool) int {
	n := 0
	for _, ok := range assigned {
		if ok {
			n++
		}
	}
	return n
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
