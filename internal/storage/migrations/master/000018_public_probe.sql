ALTER TABLE nodes ADD COLUMN last_public_probe_at TEXT;
ALTER TABLE nodes ADD COLUMN last_public_probe_result TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN last_public_probe_error TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN public_probe_network_failures INTEGER NOT NULL DEFAULT 0;
