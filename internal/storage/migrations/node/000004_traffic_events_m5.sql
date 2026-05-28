ALTER TABLE pending_traffic_events ADD COLUMN asset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE pending_traffic_events ADD COLUMN status TEXT NOT NULL DEFAULT 'completed';

CREATE INDEX IF NOT EXISTS idx_pending_traffic_unconfirmed
ON pending_traffic_events(confirmed_at, event_sequence);
