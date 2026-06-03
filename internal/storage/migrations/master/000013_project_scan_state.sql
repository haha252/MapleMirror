CREATE TABLE IF NOT EXISTS project_scan_state (
    project_id TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 0,
    config_hash TEXT NOT NULL DEFAULT '',
    last_scan_started_at TEXT,
    last_scan_completed_at TEXT,
    last_scan_id TEXT,
    last_scan_state TEXT,
    next_scan_at TEXT,
    last_error_message TEXT,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_project_scan_state_next
    ON project_scan_state(enabled, next_scan_at);
