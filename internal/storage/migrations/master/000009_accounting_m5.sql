ALTER TABLE traffic_reservations ADD COLUMN address_scope_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_reservations ADD COLUMN address_scope_key TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_reservations ADD COLUMN network_scope_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_reservations ADD COLUMN network_scope_key TEXT NOT NULL DEFAULT '';

ALTER TABLE traffic_events ADD COLUMN asset_id TEXT NOT NULL DEFAULT '';
ALTER TABLE traffic_events ADD COLUMN status TEXT NOT NULL DEFAULT 'completed';

CREATE INDEX IF NOT EXISTS idx_traffic_reservations_day_address
ON traffic_reservations(scope_day, address_scope_kind, address_scope_key);

CREATE INDEX IF NOT EXISTS idx_traffic_reservations_day_network
ON traffic_reservations(scope_day, network_scope_kind, network_scope_key);

CREATE INDEX IF NOT EXISTS idx_traffic_events_authorization
ON traffic_events(authorization_id);
