CREATE TABLE IF NOT EXISTS local_sync_tasks (
    task_id TEXT PRIMARY KEY,
    asset_id TEXT,
    task_type TEXT NOT NULL,
    state TEXT NOT NULL,
    error_message TEXT,
    updated_at TEXT NOT NULL
);
