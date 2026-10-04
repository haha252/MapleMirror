package public

import "context"

// DownloadReadyNodes needs only one routable replica per node. EXISTS avoids
// scanning every replica and deduplicating thousands of matching asset rows.
func (s Store) DownloadReadyNodes(ctx context.Context) (map[string]bool, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT n.id FROM nodes n
		WHERE n.state NOT IN ('disabled', 'offline')
		AND n.last_heartbeat_at IS NOT NULL AND n.last_heartbeat_at != ''
		AND n.public_download_base_url != ''
		AND (? <= 0 OR n.public_probe_network_failures < ?)
		AND EXISTS (
			SELECT 1 FROM node_inventory ni JOIN assets a ON a.id = ni.asset_id
			JOIN target_inventory ti ON ti.node_id = ni.node_id AND ti.asset_id = ni.asset_id
				AND ti.desired_state = 'required'
			JOIN releases r ON r.id = a.release_id AND r.selected = 1
			JOIN projects p ON p.id = r.project_id AND p.enabled = 1
			WHERE ni.node_id = n.id AND ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
			AND a.service_state = 'candidate'
		)`, s.routableAssetReplicaArgs()...)
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
