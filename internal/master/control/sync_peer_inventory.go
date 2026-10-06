package control

// Shared eligibility for whole-file replication and manifest initialization.
func syncPeerInventorySQL(asset, target string) string {
	return `		FROM node_inventory ni JOIN nodes n ON n.id = ni.node_id
		JOIN assets a ON a.id = ni.asset_id
		JOIN releases r ON r.id = a.release_id
		JOIN projects p ON p.id = r.project_id
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = ni.asset_id AND ti.desired_state = 'required'
		WHERE ni.asset_id = ` + asset + ` AND ni.node_id != ` + target + ` AND n.state != 'disabled'
		AND n.state != 'offline' AND n.last_heartbeat_at IS NOT NULL
		AND n.last_heartbeat_at != '' AND n.public_download_base_url != '' AND ni.state = 'verified'
		AND ni.local_digest_sha256 = a.digest_sha256 AND ni.size_bytes = a.size_bytes
		AND a.service_state IN ('candidate', 'pending', 'active') AND r.selected = 1 AND p.enabled = 1`
}
