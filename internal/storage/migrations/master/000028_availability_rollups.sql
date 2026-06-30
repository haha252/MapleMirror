CREATE TABLE IF NOT EXISTS node_availability_rollups (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    bucket_start TEXT NOT NULL,
    bucket_minutes INTEGER NOT NULL,
    total_samples INTEGER NOT NULL,
    ok_samples INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (node_id, bucket_start, bucket_minutes)
);

CREATE INDEX IF NOT EXISTS idx_node_availability_rollups_window
ON node_availability_rollups(bucket_start, node_id);
