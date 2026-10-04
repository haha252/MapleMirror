package public

import "context"

// DownloadReadyNodes uses the public routing conditions without calculating
// historical SLA or traffic totals for the admin's frequent live refreshes.
func (s Store) DownloadReadyNodes(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT n.id FROM assets a `+
		routableAssetReplicaSQL+`
		JOIN releases r ON r.id = a.release_id AND r.selected = 1
		JOIN projects p ON p.id = r.project_id AND p.enabled = 1
		WHERE a.service_state = 'candidate'`, s.routableAssetReplicaArgs()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}
