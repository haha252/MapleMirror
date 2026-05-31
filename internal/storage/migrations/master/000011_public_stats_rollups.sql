CREATE TABLE IF NOT EXISTS daily_site_stats (
    stat_day TEXT PRIMARY KEY,
    page_views INTEGER NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS daily_asset_stats (
    stat_day TEXT NOT NULL,
    asset_id TEXT NOT NULL REFERENCES assets(id),
    authorization_count INTEGER NOT NULL,
    transfer_started_count INTEGER NOT NULL,
    sent_bytes INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (stat_day, asset_id)
);

CREATE TABLE IF NOT EXISTS daily_node_traffic_stats (
    stat_day TEXT NOT NULL,
    node_id TEXT NOT NULL REFERENCES nodes(id),
    sent_bytes INTEGER NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (stat_day, node_id)
);
