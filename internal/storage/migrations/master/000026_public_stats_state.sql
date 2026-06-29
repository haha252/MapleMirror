CREATE TABLE IF NOT EXISTS public_stat_totals (
    id TEXT PRIMARY KEY,
    page_views INTEGER NOT NULL DEFAULT 0,
    authorization_count INTEGER NOT NULL DEFAULT 0,
    web_authorization_count INTEGER NOT NULL DEFAULT 0,
    api_authorization_count INTEGER NOT NULL DEFAULT 0,
    transfer_started_count INTEGER NOT NULL DEFAULT 0,
    sent_bytes INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS daily_public_stats (
    stat_day TEXT PRIMARY KEY,
    page_views INTEGER NOT NULL DEFAULT 0,
    authorization_count INTEGER NOT NULL DEFAULT 0,
    web_authorization_count INTEGER NOT NULL DEFAULT 0,
    api_authorization_count INTEGER NOT NULL DEFAULT 0,
    transfer_started_count INTEGER NOT NULL DEFAULT 0,
    sent_bytes INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS asset_stat_totals (
    asset_id TEXT PRIMARY KEY REFERENCES assets(id),
    authorization_count INTEGER NOT NULL DEFAULT 0,
    web_authorization_count INTEGER NOT NULL DEFAULT 0,
    api_authorization_count INTEGER NOT NULL DEFAULT 0,
    transfer_started_count INTEGER NOT NULL DEFAULT 0,
    sent_bytes INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS node_traffic_totals (
    node_id TEXT PRIMARY KEY REFERENCES nodes(id),
    sent_bytes INTEGER NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_asset_stat_totals_downloads
ON asset_stat_totals(authorization_count DESC, asset_id);
