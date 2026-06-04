CREATE TABLE IF NOT EXISTS client_blocks (
    client_prefix_key TEXT PRIMARY KEY,
    reason TEXT NOT NULL,
    source TEXT NOT NULL,
    blocked_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    attempts_after_block INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_client_blocks_expires
ON client_blocks(expires_at);
