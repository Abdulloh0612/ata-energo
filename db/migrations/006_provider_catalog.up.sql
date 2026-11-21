CREATE TABLE providers (
    code TEXT PRIMARY KEY CHECK (code ~ '^[a-z][a-z0-9_-]{1,29}$'),
    name TEXT NOT NULL,
    adapter TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    mock BOOLEAN NOT NULL DEFAULT false
);
INSERT INTO providers VALUES
    ('deye','Deye','deye',true,false),
    ('eybond','Eybond / Solar of Things','eybond',false,true),
    ('local','Локальный мониторинг','local',true,false);
ALTER TABLE vendor_accounts ADD CONSTRAINT vendor_provider FOREIGN KEY (vendor) REFERENCES providers(code);
CREATE TABLE provider_sites (
    id BIGSERIAL PRIMARY KEY,
    provider_code TEXT NOT NULL REFERENCES providers(code),
    credential_id BIGINT REFERENCES vendor_accounts(id) ON DELETE RESTRICT,
    external_id TEXT NOT NULL,
    client_id BIGINT REFERENCES clients(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    address TEXT NOT NULL DEFAULT '',
    timezone TEXT NOT NULL DEFAULT 'Asia/Bishkek',
    latitude DOUBLE PRECISION,
    longitude DOUBLE PRECISION,
    status TEXT NOT NULL,
    archived BOOLEAN NOT NULL DEFAULT false,
    last_synced_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX provider_site_identity ON provider_sites(provider_code,credential_id,external_id) NULLS NOT DISTINCT;
CREATE INDEX provider_sites_client_idx ON provider_sites(client_id);
CREATE TABLE provider_catalog_sync (
    account_id BIGINT PRIMARY KEY REFERENCES vendor_accounts(id) ON DELETE CASCADE,
    last_attempt_at TIMESTAMPTZ NOT NULL,
    last_success_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT ''
);
