CREATE TABLE IF NOT EXISTS pending_sync_task_results (
    task_id TEXT PRIMARY KEY,
    asset_id TEXT,
    result TEXT NOT NULL,
    local_digest_sha256 TEXT,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    message TEXT,
    created_at TEXT NOT NULL,
    reported_at TEXT
);
