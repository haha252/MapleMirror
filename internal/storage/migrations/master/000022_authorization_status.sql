ALTER TABLE download_authorizations ADD COLUMN status_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE download_authorizations ADD COLUMN status_updated_at TEXT NOT NULL DEFAULT '';
