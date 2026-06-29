CREATE TABLE IF NOT EXISTS runtime_kv (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    repository TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    homepage_url TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL,
    retain_versions INTEGER NOT NULL,
    include_prerelease INTEGER NOT NULL,
    download_multiplier INTEGER NOT NULL,
    config_hash TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS releases (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id),
    github_release_id INTEGER NOT NULL,
    tag_name TEXT NOT NULL,
    prerelease INTEGER NOT NULL,
    published_at TEXT NOT NULL,
    selected INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(project_id, github_release_id)
);
CREATE INDEX IF NOT EXISTS idx_releases_project_published ON releases(project_id, published_at);

CREATE TABLE IF NOT EXISTS assets (
    id TEXT PRIMARY KEY,
    release_id TEXT NOT NULL REFERENCES releases(id),
    github_asset_id INTEGER NOT NULL,
    file_name TEXT NOT NULL,
    architecture TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    source_url TEXT NOT NULL,
    digest_sha256 TEXT NOT NULL,
    service_state TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(release_id, github_asset_id)
);
CREATE INDEX IF NOT EXISTS idx_assets_release_state ON assets(release_id, service_state);

CREATE TABLE IF NOT EXISTS nodes (
    id TEXT PRIMARY KEY,
    public_name TEXT NOT NULL,
    certificate_fingerprint TEXT,
    state TEXT NOT NULL,
    target_bandwidth_bps INTEGER NOT NULL,
    last_heartbeat_at TEXT,
    routing_ready INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
