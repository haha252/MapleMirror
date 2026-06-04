CREATE TABLE IF NOT EXISTS admin_web_sessions (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL,
    ip_key TEXT NOT NULL,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_admin_web_sessions_expires
ON admin_web_sessions(expires_at);

CREATE TABLE IF NOT EXISTS admin_login_failures (
    ip_key TEXT PRIMARY KEY,
    masked_ip TEXT NOT NULL,
    failed_count INTEGER NOT NULL,
    window_started_at TEXT NOT NULL,
    last_failed_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_ip_blocks (
    ip_key TEXT PRIMARY KEY,
    masked_ip TEXT NOT NULL,
    reason TEXT NOT NULL,
    blocked_at TEXT NOT NULL,
    expires_at TEXT NOT NULL,
    attempts_after_block INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_admin_ip_blocks_expires
ON admin_ip_blocks(expires_at);
