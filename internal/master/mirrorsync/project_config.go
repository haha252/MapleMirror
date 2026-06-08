package mirrorsync

import (
	"context"
	"database/sql"

	"mirror-server/internal/config"
	"mirror-server/internal/master/assignment"
)

func (s Store) SyncProjectConfig(ctx context.Context, projects config.Projects) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := nowText()
	seen := make([]string, 0, len(projects.Projects))
	for _, project := range projects.Projects {
		if err := upsertProjectConfig(ctx, tx, project, now); err != nil {
			return err
		}
		if err := upsertProjectScanState(ctx, tx, project, now); err != nil {
			return err
		}
		seen = append(seen, project.ID)
		if !project.Enabled {
			if err := disableProjectTargets(ctx, tx, project.ID, now); err != nil {
				return err
			}
		}
	}
	if err := disableMissingProjects(ctx, tx, seen, now); err != nil {
		return err
	}
	return tx.Commit()
}

func upsertProjectConfig(ctx context.Context, tx *sql.Tx, project config.Project, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, name, repository, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name,
		repository = excluded.repository, enabled = excluded.enabled,
		retain_versions = excluded.retain_versions,
		include_prerelease = excluded.include_prerelease,
		download_multiplier = excluded.download_multiplier,
		config_hash = excluded.config_hash, updated_at = excluded.updated_at`,
		project.ID, project.Name, project.Repository, boolInt(project.Enabled),
		project.RetainVersions, boolInt(project.IncludePrerelease),
		project.DownloadMultiplier, projectHash(project), now)
	return err
}

func disableProjectTargets(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE releases SET selected = 0 WHERE project_id = ?`, projectID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE asset_id IN (
		SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ?)`, now, projectID)
	if err != nil {
		return err
	}
	if err := assignment.CancelObsoleteProjectTasks(ctx, tx, projectID, now); err != nil {
		return err
	}
	_, err = assignment.GenerateDeleteTasks(ctx, tx, now)
	return err
}

func disableMissingProjects(ctx context.Context, tx *sql.Tx, seen []string, now string) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM projects WHERE enabled = 1`)
	if err != nil {
		return err
	}
	defer rows.Close()

	keep := map[string]bool{}
	for _, id := range seen {
		keep[id] = true
	}
	var missing []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if !keep[id] {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range missing {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET enabled = 0,
			updated_at = ? WHERE id = ?`, now, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE project_scan_state SET enabled = 0,
			updated_at = ? WHERE project_id = ?`, now, id); err != nil {
			return err
		}
		if err := disableProjectTargets(ctx, tx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func upsertProjectScanState(ctx context.Context, tx *sql.Tx, project config.Project, now string) error {
	hash := projectHash(project)
	exists, previousHash, err := projectScanStateExists(ctx, tx, project.ID)
	if err != nil {
		return err
	}
	nextScan := any(nil)
	if project.Enabled && (!exists || previousHash != hash) {
		nextScan = now
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO project_scan_state
		(project_id, enabled, config_hash, next_scan_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		enabled = excluded.enabled,
		config_hash = excluded.config_hash,
		next_scan_at = CASE
			WHEN excluded.enabled = 0 THEN NULL
			WHEN project_scan_state.config_hash != excluded.config_hash THEN excluded.next_scan_at
			ELSE project_scan_state.next_scan_at
		END,
		updated_at = excluded.updated_at`,
		project.ID, boolInt(project.Enabled), hash, nextScan, now)
	return err
}
