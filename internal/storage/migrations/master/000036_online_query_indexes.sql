CREATE INDEX IF NOT EXISTS idx_download_authorizations_pending_delivery
ON download_authorizations(node_id, issued_at, id)
WHERE token_hash != '' AND delivered_at = '' AND status IN ('issued', 'active');

CREATE INDEX IF NOT EXISTS idx_target_inventory_required_counts
ON target_inventory(node_id, asset_id) WHERE desired_state = 'required';

CREATE INDEX IF NOT EXISTS idx_node_inventory_live_counts
ON node_inventory(node_id, state) WHERE state IN ('verified', 'mismatch');

CREATE INDEX IF NOT EXISTS idx_node_tasks_live_counts
ON node_tasks(node_id, state) WHERE state IN ('pending', 'sent', 'running', 'retry_wait', 'failed');

CREATE INDEX IF NOT EXISTS idx_node_availability_rollups_counts
ON node_availability_rollups(node_id, bucket_start, total_samples, ok_samples);
