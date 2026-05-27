CREATE TABLE IF NOT EXISTS pairing_codes (
    id TEXT PRIMARY KEY,
    code_hash TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    used_at TEXT,
    created_by_request_id TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS node_tasks (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    task_type TEXT NOT NULL,
    asset_id TEXT REFERENCES assets(id),
    state TEXT NOT NULL,
    request_id TEXT NOT NULL,
    created_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS node_heartbeats (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    state TEXT NOT NULL,
    pressure_ratio REAL NOT NULL,
    active_downloads INTEGER NOT NULL,
    free_bytes INTEGER NOT NULL,
    reported_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_audit_events (
    id TEXT PRIMARY KEY,
    operation TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    result TEXT NOT NULL,
    request_id TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS challenges (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('altcha', 'api_pow')),
    asset_id TEXT NOT NULL REFERENCES assets(id),
    client_prefix_key TEXT NOT NULL,
    nonce_hash TEXT NOT NULL,
    difficulty INTEGER,
    expires_at TEXT NOT NULL,
    consumed_at TEXT,
    request_id TEXT NOT NULL
);
