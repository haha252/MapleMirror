package mirrorsync

import (
	"context"
	"database/sql"

	"mirror-server/internal/config"
)

type projectConfigState struct {
	id, name, repository, description, homepage, hash string
	enabled, retain, prerelease, multiplier           int
	scanEnabled                                       sql.NullInt64
	scanHash, nextScan                                sql.NullString
}

func (s projectConfigState) matches(p config.Project, hash string) bool {
	return s.id == p.ID && s.name == p.Name && s.repository == p.Repository &&
		s.description == p.Description && s.homepage == p.HomepageURL && s.hash == hash &&
		s.enabled == boolInt(p.Enabled) && s.retain == p.RetainVersions &&
		s.prerelease == boolInt(p.IncludePrerelease) && s.multiplier == p.DownloadMultiplier
}

func (s projectConfigState) scanMatches(p config.Project, hash string) bool {
	return s.scanEnabled.Valid && s.scanEnabled.Int64 == int64(boolInt(p.Enabled)) &&
		s.scanHash.Valid && s.scanHash.String == hash && (p.Enabled || s.nextScan.String == "")
}

// Read current database state in the same transaction instead of caching it:
// resets, failed previous syncs, and externally changed rows remain observable.
func loadProjectConfigState(ctx context.Context, tx *sql.Tx) (map[string]projectConfigState, error) {
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.name,p.repository,p.description,p.homepage_url,
		p.config_hash,p.enabled,p.retain_versions,p.include_prerelease,p.download_multiplier,
		ps.enabled,ps.config_hash,ps.next_scan_at FROM projects p
		LEFT JOIN project_scan_state ps ON ps.project_id=p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]projectConfigState{}
	for rows.Next() {
		var state projectConfigState
		if err := rows.Scan(&state.id, &state.name, &state.repository, &state.description, &state.homepage,
			&state.hash, &state.enabled, &state.retain, &state.prerelease, &state.multiplier,
			&state.scanEnabled, &state.scanHash, &state.nextScan); err != nil {
			return nil, err
		}
		out[state.id] = state
	}
	return out, rows.Err()
}
