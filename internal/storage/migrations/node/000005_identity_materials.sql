ALTER TABLE control_identity ADD COLUMN certificate_pem TEXT;
ALTER TABLE control_identity ADD COLUMN ca_pem TEXT;
ALTER TABLE control_identity ADD COLUMN private_key_pem TEXT;
ALTER TABLE control_identity ADD COLUMN download_token_public_key_pem TEXT;

CREATE TABLE IF NOT EXISTS node_enrollment_state (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    enrollment_id TEXT,
    pairing_code TEXT,
    private_key_pem TEXT,
    ca_pem TEXT,
    updated_at TEXT NOT NULL
);
