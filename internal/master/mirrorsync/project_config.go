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
	changedProjects := map[string]bool{}
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
			changedProjects[project.ID] = true
		}
	}
	missing, err := disableMissingProjects(ctx, tx, seen, now)
	if err != nil {
		return err
	}
	for _, projectID := range missing {
		changedProjects[projectID] = true
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for projectID := range changedProjects {
		s.NotifyProjectTaskNodes(ctx, projectID)
	}
	return nil
}

func upsertProjectConfig(ctx context.Context, tx *sql.Tx, project config.Project, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, name, repository, description, homepage_url, enabled, retain_versions, include_prerelease,
		download_multiplier, config_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET name = excluded.name,
			repository = excluded.repository,
			description = excluded.description,
			homepage_url = excluded.homepage_url,
			enabled = excluded.enabled,
			retain_versions = excluded.retain_versions,
			include_prerelease = excluded.include_prerelease,
			download_multiplier = excluded.download_multiplier,
			config_hash = excluded.config_hash, updated_at = excluded.updated_at
			WHERE projects.name != excluded.name
			OR projects.repository != excluded.repository
			OR projects.description != excluded.description
			OR projects.homepage_url != excluded.homepage_url
			OR projects.enabled != excluded.enabled
			OR projects.retain_versions != excluded.retain_versions
			OR projects.include_prerelease != excluded.include_prerelease
			OR projects.download_multiplier != excluded.download_multiplier
			OR projects.config_hash != excluded.config_hash`,
		project.ID, project.Name, project.Repository, project.Description,
		project.HomepageURL, boolInt(project.Enabled),
		project.RetainVersions, boolInt(project.IncludePrerelease),
		project.DownloadMultiplier, projectHash(project), now)
	return err
}

func disableProjectTargets(ctx context.Context, tx *sql.Tx, projectID, now string) error {
	_, err := tx.ExecContext(ctx, `UPDATE releases SET selected = 0
		WHERE project_id = ? AND selected != 0`, projectID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE target_inventory SET desired_state = 'remove',
		updated_at = ? WHERE asset_id IN (
		SELECT a.id FROM assets a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = ?) AND desired_state != 'remove'`, now, projectID)
	if err != nil {
		return err
	}
	if err := assignment.CancelObsoleteProjectTasks(ctx, tx, projectID, now); err != nil {
		return err
	}
	_, err = assignment.GenerateDeleteTasks(ctx, tx, now)
	return err
}

func disableMissingProjects(ctx context.Context, tx *sql.Tx, seen []string, now string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM projects WHERE enabled = 1`)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		if !keep[id] {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range missing {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET enabled = 0,
			updated_at = ? WHERE id = ? AND enabled != 0`, now, id); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE project_scan_state SET enabled = 0,
			updated_at = ? WHERE project_id = ? AND enabled != 0`, now, id); err != nil {
			return nil, err
		}
		if err := disableProjectTargets(ctx, tx, id, now); err != nil {
			return nil, err
		}
	}
	return missing, nil
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
		updated_at = excluded.updated_at
		WHERE project_scan_state.enabled != excluded.enabled
		OR project_scan_state.config_hash != excluded.config_hash
		OR COALESCE(project_scan_state.next_scan_at, '') != COALESCE(CASE
			WHEN excluded.enabled = 0 THEN NULL
			WHEN project_scan_state.config_hash != excluded.config_hash THEN excluded.next_scan_at
			ELSE project_scan_state.next_scan_at
		END, '')`,
		project.ID, boolInt(project.Enabled), hash, nextScan, now)
	return err
}
