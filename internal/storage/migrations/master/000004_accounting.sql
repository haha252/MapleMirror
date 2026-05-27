CREATE TABLE IF NOT EXISTS download_authorizations (
    id TEXT PRIMARY KEY,
    asset_id TEXT NOT NULL REFERENCES assets(id),
    node_id TEXT NOT NULL REFERENCES nodes(id),
    client_prefix_key TEXT NOT NULL,
    issued_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    max_bytes INTEGER NOT NULL,
    range_limit INTEGER NOT NULL,
    status TEXT NOT NULL,
    request_id TEXT NOT NULL,
    first_transfer_at TEXT
);

CREATE TABLE IF NOT EXISTS quota_buckets (
    scope_kind TEXT NOT NULL,
    scope_key TEXT NOT NULL,
    tokens_microunits INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (scope_kind, scope_key)
);

CREATE TABLE IF NOT EXISTS traffic_reservations (
    authorization_id TEXT PRIMARY KEY REFERENCES download_authorizations(id),
    scope_day TEXT NOT NULL,
    address_reserved_bytes INTEGER NOT NULL,
    network_reserved_bytes INTEGER NOT NULL,
    settled_bytes INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS traffic_events (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    event_sequence INTEGER NOT NULL,
    authorization_id TEXT NOT NULL REFERENCES download_authorizations(id),
    node_request_id TEXT NOT NULL,
    master_request_id TEXT NOT NULL,
    sent_bytes INTEGER NOT NULL,
    reported_at TEXT NOT NULL,
    accounted_at TEXT,
    PRIMARY KEY (node_id, event_sequence)
);

CREATE TABLE IF NOT EXISTS daily_traffic_stats (
    stat_day TEXT NOT NULL,
    scope_kind TEXT NOT NULL,
    scope_key TEXT NOT NULL,
    sent_bytes INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (stat_day, scope_kind, scope_key)
);

CREATE TABLE IF NOT EXISTS daily_project_stats (
    stat_day TEXT NOT NULL,
    project_id TEXT NOT NULL REFERENCES projects(id),
    authorization_count INTEGER NOT NULL,
    transfer_started_count INTEGER NOT NULL,
    sent_bytes INTEGER NOT NULL,
    PRIMARY KEY (stat_day, project_id)
);

CREATE TABLE IF NOT EXISTS node_availability_samples (
    node_id TEXT NOT NULL REFERENCES nodes(id),
    sample_start TEXT NOT NULL,
    sample_end TEXT NOT NULL,
    routable INTEGER NOT NULL,
    heartbeat_ok INTEGER NOT NULL,
    PRIMARY KEY (node_id, sample_start)
);
