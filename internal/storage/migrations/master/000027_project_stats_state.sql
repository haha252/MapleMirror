CREATE TABLE IF NOT EXISTS project_stat_totals (
    project_id TEXT PRIMARY KEY REFERENCES projects(id),
    authorization_count INTEGER NOT NULL DEFAULT 0,
    web_authorization_count INTEGER NOT NULL DEFAULT 0,
    api_authorization_count INTEGER NOT NULL DEFAULT 0,
    transfer_started_count INTEGER NOT NULL DEFAULT 0,
    sent_bytes INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_project_stat_totals_downloads
ON project_stat_totals(authorization_count DESC, project_id);
