ALTER TABLE download_authorizations ADD COLUMN token_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE download_authorizations ADD COLUMN traffic_limit_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE download_authorizations ADD COLUMN first_connection_timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE download_authorizations ADD COLUMN idle_timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE download_authorizations ADD COLUMN max_duration_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE download_authorizations ADD COLUMN delivered_at TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_download_authorizations_delivery
ON download_authorizations(node_id, delivered_at, issued_at);
