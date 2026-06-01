package public

const routableAssetReplicaSQL = `
		JOIN node_inventory ni ON ni.asset_id = a.id
			AND ni.state = 'verified'
			AND ni.local_digest_sha256 = a.digest_sha256
			AND ni.size_bytes = a.size_bytes
		JOIN nodes n ON n.id = ni.node_id
			AND n.state NOT IN ('disabled', 'offline')
			AND n.last_heartbeat_at IS NOT NULL
			AND n.last_heartbeat_at != ''
			AND n.public_download_base_url != ''
			AND ni.verified_at >= n.last_heartbeat_at`
