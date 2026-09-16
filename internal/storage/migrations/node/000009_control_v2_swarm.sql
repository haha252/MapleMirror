ALTER TABLE local_sync_tasks ADD COLUMN attempt_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pending_sync_task_results ADD COLUMN attempt_id TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS swarm_partials (
    asset_id TEXT NOT NULL,
    manifest_id TEXT NOT NULL,
    partial_path TEXT NOT NULL,
    asset_size INTEGER NOT NULL CHECK(asset_size >= 0),
    piece_size INTEGER NOT NULL CHECK(piece_size > 0),
    piece_count INTEGER NOT NULL CHECK(piece_count > 0 AND piece_count <= 8192),
    verified_bitmap BLOB NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_access_at TEXT NOT NULL,
    PRIMARY KEY(asset_id, manifest_id)
);
CREATE INDEX IF NOT EXISTS idx_swarm_partials_access
    ON swarm_partials(last_access_at);

CREATE TABLE IF NOT EXISTS pending_swarm_manifests (
    manifest_id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    manifest_json BLOB NOT NULL,
    result_json BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pending_swarm_manifests_task
    ON pending_swarm_manifests(task_id, attempt_id);
