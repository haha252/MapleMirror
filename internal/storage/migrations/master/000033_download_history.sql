CREATE TABLE IF NOT EXISTS download_history (
    authorization_id TEXT PRIMARY KEY,
    client_prefix_key TEXT NOT NULL,
    source_kind TEXT NOT NULL DEFAULT 'web',
    project_id TEXT NOT NULL,
    project_name TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    file_name TEXT NOT NULL,
    version TEXT NOT NULL,
    system TEXT NOT NULL,
    architecture TEXT NOT NULL,
    node_id TEXT NOT NULL,
    node_name TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    first_transfer_at TEXT NOT NULL DEFAULT '',
    last_transfer_at TEXT NOT NULL DEFAULT '',
    sent_bytes INTEGER NOT NULL DEFAULT 0 CHECK(sent_bytes >= 0),
    status TEXT NOT NULL DEFAULT 'issued',
    status_reason TEXT NOT NULL DEFAULT '',
    request_id TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_download_history_client_issued
ON download_history(client_prefix_key, issued_at DESC);

CREATE INDEX IF NOT EXISTS idx_download_history_issued
ON download_history(issued_at);

CREATE INDEX IF NOT EXISTS idx_download_history_project_issued
ON download_history(project_id, issued_at DESC);
