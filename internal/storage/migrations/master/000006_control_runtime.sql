CREATE TABLE IF NOT EXISTS node_control_sessions (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    certificate_id TEXT REFERENCES node_certificates(id),
    request_id TEXT NOT NULL,
    connected_at TEXT NOT NULL,
    last_message_sequence INTEGER NOT NULL DEFAULT 0,
    last_heartbeat_at TEXT,
    disconnected_at TEXT,
    close_reason TEXT
);
CREATE INDEX IF NOT EXISTS idx_control_sessions_node_active
    ON node_control_sessions(node_id, disconnected_at);
CREATE INDEX IF NOT EXISTS idx_control_sessions_heartbeat
    ON node_control_sessions(node_id, last_heartbeat_at);

CREATE TABLE IF NOT EXISTS node_inventory_reports (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    revision INTEGER NOT NULL,
    complete INTEGER NOT NULL,
    item_count INTEGER NOT NULL,
    result TEXT NOT NULL,
    request_id TEXT NOT NULL,
    reported_at TEXT NOT NULL,
    UNIQUE(node_id, revision)
);
CREATE INDEX IF NOT EXISTS idx_inventory_reports_node
    ON node_inventory_reports(node_id, reported_at);

CREATE TABLE IF NOT EXISTS node_pressure_reports (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    pressure_ratio REAL NOT NULL,
    active_downloads INTEGER NOT NULL,
    free_bytes INTEGER NOT NULL,
    request_id TEXT NOT NULL,
    reported_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pressure_reports_node
    ON node_pressure_reports(node_id, reported_at);
