ALTER TABLE local_authorizations ADD COLUMN token_hash TEXT NOT NULL DEFAULT '';
ALTER TABLE local_authorizations ADD COLUMN client_prefix_key TEXT NOT NULL DEFAULT '';
ALTER TABLE local_authorizations ADD COLUMN max_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE local_authorizations ADD COLUMN traffic_limit_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE local_authorizations ADD COLUMN range_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE local_authorizations ADD COLUMN request_id TEXT NOT NULL DEFAULT '';
ALTER TABLE local_authorizations ADD COLUMN first_connection_timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE local_authorizations ADD COLUMN idle_timeout_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE local_authorizations ADD COLUMN max_duration_seconds INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_local_authorizations_token_hash
ON local_authorizations(token_hash);
