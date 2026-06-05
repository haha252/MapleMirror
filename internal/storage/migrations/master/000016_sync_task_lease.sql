ALTER TABLE node_tasks ADD COLUMN lease_expires_at TEXT;

CREATE INDEX IF NOT EXISTS idx_node_tasks_dispatch
    ON node_tasks(node_id, state, retry_after, created_at);

CREATE INDEX IF NOT EXISTS idx_node_tasks_asset_state
    ON node_tasks(node_id, asset_id, task_type, state);

CREATE INDEX IF NOT EXISTS idx_node_inventory_asset_state
    ON node_inventory(asset_id, state);
