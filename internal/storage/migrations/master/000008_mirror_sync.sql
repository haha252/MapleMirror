CREATE TABLE IF NOT EXISTS sync_scans (
    id TEXT PRIMARY KEY,
    project_id TEXT,
    state TEXT NOT NULL,
    selected_releases INTEGER NOT NULL DEFAULT 0,
    accepted_assets INTEGER NOT NULL DEFAULT 0,
    rejected_assets INTEGER NOT NULL DEFAULT 0,
    error_message TEXT,
    request_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    next_allowed_scan_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_sync_scans_project_started
    ON sync_scans(project_id, started_at);

ALTER TABLE node_tasks ADD COLUMN error_message TEXT;
ALTER TABLE node_tasks ADD COLUMN attempts INTEGER NOT NULL DEFAULT 0;
ALTER TABLE node_tasks ADD COLUMN updated_at TEXT;
ALTER TABLE node_tasks ADD COLUMN retry_after TEXT;
