CREATE TABLE IF NOT EXISTS local_assets (
    asset_id TEXT PRIMARY KEY,
    relative_path TEXT NOT NULL,
    digest_sha256 TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    verified_at TEXT,
    state TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS pending_traffic_events (
    event_sequence INTEGER PRIMARY KEY,
    authorization_id TEXT NOT NULL,
    node_request_id TEXT NOT NULL,
    master_request_id TEXT NOT NULL,
    sent_bytes INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    confirmed_at TEXT
);
