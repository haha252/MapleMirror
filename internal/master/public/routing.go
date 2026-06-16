package public

const routableAssetReplicaSQL = `
		JOIN node_inventory ni ON ni.asset_id = a.id
			AND ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256
			AND ni.size_bytes = a.size_bytes
		JOIN target_inventory ti ON ti.node_id = ni.node_id
			AND ti.asset_id = a.id
			AND ti.desired_state = 'required'
		JOIN nodes n ON n.id = ni.node_id
			AND n.state NOT IN ('disabled', 'offline')
			AND n.last_heartbeat_at IS NOT NULL
			AND n.last_heartbeat_at != ''
			AND n.public_download_base_url != ''
			AND (? <= 0 OR n.public_probe_network_failures < ?)`

func (s Store) routableAssetReplicaArgs() []any {
	threshold := s.PublicProbeNetworkFailures
	return []any{threshold, threshold}
}
