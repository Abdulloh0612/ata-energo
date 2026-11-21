CREATE TABLE crm_work (
    id BIGSERIAL PRIMARY KEY,
    demo_key TEXT UNIQUE,
    kind TEXT NOT NULL CHECK (kind IN ('requests','tasks')),
    title TEXT NOT NULL CHECK (length(trim(title)) BETWEEN 1 AND 160),
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','progress','closed')),
    priority TEXT NOT NULL DEFAULT 'normal' CHECK (priority IN ('low','normal','high')),
    client_id BIGINT REFERENCES clients(id) ON DELETE SET NULL,
    device_id BIGINT REFERENCES devices(id) ON DELETE SET NULL,
    assignee_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_by BIGINT REFERENCES users(id) ON DELETE SET NULL,
    due_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX crm_work_kind_idx ON crm_work (kind, id DESC);
CREATE INDEX crm_work_assignee_idx ON crm_work (assignee_id, kind);
CREATE TABLE crm_activity (
    id BIGSERIAL PRIMARY KEY,
    work_id BIGINT REFERENCES crm_work(id) ON DELETE SET NULL,
    actor_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    action TEXT NOT NULL,
    snapshot JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX crm_activity_work_idx ON crm_activity (work_id, id DESC);
