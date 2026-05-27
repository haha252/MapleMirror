CREATE TABLE IF NOT EXISTS target_inventory (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    asset_id TEXT NOT NULL REFERENCES assets(id),
    desired_state TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (node_id, asset_id)
);
CREATE INDEX IF NOT EXISTS idx_target_inventory_state ON target_inventory(node_id, desired_state);

CREATE TABLE IF NOT EXISTS node_inventory (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    asset_id TEXT NOT NULL REFERENCES assets(id),
    local_digest_sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    verified_at TEXT,
    state TEXT NOT NULL,
    PRIMARY KEY (node_id, asset_id)
);
CREATE INDEX IF NOT EXISTS idx_node_inventory_state ON node_inventory(node_id, state);
