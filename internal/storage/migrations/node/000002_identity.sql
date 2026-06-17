CREATE TABLE IF NOT EXISTS control_identity (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    node_id TEXT NOT NULL,
    certificate_fingerprint TEXT NOT NULL,
    certificate_not_after TEXT NOT NULL,
    enrolled_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS control_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    next_sequence INTEGER NOT NULL,
    last_ack_sequence INTEGER NOT NULL,
    last_session_id TEXT,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS inventory_report_cursor (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    next_revision INTEGER NOT NULL,
    last_acked_revision INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    force_report_requested_at TEXT
);
