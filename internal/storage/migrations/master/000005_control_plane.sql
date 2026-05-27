ALTER TABLE pairing_codes ADD COLUMN code_hint TEXT;
ALTER TABLE pairing_codes ADD COLUMN revoked_at TEXT;
ALTER TABLE pairing_codes ADD COLUMN consumed_by_request_id TEXT;
ALTER TABLE pairing_codes ADD COLUMN created_at TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_pairing_codes_hash ON pairing_codes(code_hash);

ALTER TABLE admin_audit_events ADD COLUMN admin_identity TEXT;
ALTER TABLE admin_audit_events ADD COLUMN details_summary TEXT;

CREATE TABLE IF NOT EXISTS node_enrollment_requests (
    id TEXT PRIMARY KEY,
    pairing_code_id TEXT NOT NULL REFERENCES pairing_codes(id),
    public_name TEXT NOT NULL,
    csr_pem TEXT NOT NULL,
    public_key_fingerprint TEXT NOT NULL,
    status TEXT NOT NULL,
    request_id TEXT NOT NULL,
    capabilities_json TEXT NOT NULL DEFAULT '[]',
    expires_at TEXT NOT NULL,
    approved_at TEXT,
    rejected_at TEXT,
    collected_at TEXT,
    created_at TEXT NOT NULL,
    UNIQUE(pairing_code_id)
);
CREATE INDEX IF NOT EXISTS idx_enroll_status ON node_enrollment_requests(status, expires_at);

CREATE TABLE IF NOT EXISTS node_certificates (
    id TEXT PRIMARY KEY,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    serial_number TEXT NOT NULL UNIQUE,
    fingerprint TEXT NOT NULL UNIQUE,
    not_before TEXT NOT NULL,
    not_after TEXT NOT NULL,
    status TEXT NOT NULL,
    issued_request_id TEXT NOT NULL,
    revoked_at TEXT,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_node_certificates_node ON node_certificates(node_id, status);
