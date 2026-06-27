ALTER TABLE download_authorizations ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'web' CHECK (source_kind IN ('web', 'api'));

ALTER TABLE daily_project_stats ADD COLUMN web_authorization_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_project_stats ADD COLUMN api_authorization_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE daily_asset_stats ADD COLUMN web_authorization_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE daily_asset_stats ADD COLUMN api_authorization_count INTEGER NOT NULL DEFAULT 0;
