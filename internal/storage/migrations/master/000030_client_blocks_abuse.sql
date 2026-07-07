ALTER TABLE client_blocks ADD COLUMN escalation_level INTEGER NOT NULL DEFAULT 0;
ALTER TABLE client_blocks ADD COLUMN punishment_active INTEGER NOT NULL DEFAULT 0;
