-- Ата Энерго: пользователи, клиенты, модемы, устройства, показания, аварии

CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    username      VARCHAR(150) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name    VARCHAR(150) NOT NULL DEFAULT '',
    last_name     VARCHAR(150) NOT NULL DEFAULT '',
    email         VARCHAR(254) NOT NULL DEFAULT '',
    phone         VARCHAR(30)  NOT NULL DEFAULT '',
    position      VARCHAR(100) NOT NULL DEFAULT '',
    role          VARCHAR(20)  NOT NULL DEFAULT 'user',
    is_active     BOOLEAN      NOT NULL DEFAULT TRUE,
    is_superuser  BOOLEAN      NOT NULL DEFAULT FALSE,
    date_joined   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_login    TIMESTAMPTZ
);

CREATE INDEX idx_users_role ON users(role);

CREATE TABLE clients (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT UNIQUE REFERENCES users(id) ON DELETE SET NULL,
    name       VARCHAR(200) NOT NULL,
    phone      VARCHAR(30)  NOT NULL DEFAULT '',
    address    VARCHAR(300) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_clients_name ON clients(name);

-- Локальные агенты поиска инверторов
CREATE TABLE agents (
    id           BIGSERIAL PRIMARY KEY,
    name         VARCHAR(100) NOT NULL,
    client_id    BIGINT REFERENCES clients(id) ON DELETE SET NULL,
    token_hash   VARCHAR(64) UNIQUE NOT NULL,
    token_hint   VARCHAR(8)   NOT NULL DEFAULT '',
    hostname     VARCHAR(200) NOT NULL DEFAULT '',
    ip           VARCHAR(64)  NOT NULL DEFAULT '',
    local_ip     VARCHAR(64)  NOT NULL DEFAULT '',
    platform     VARCHAR(200) NOT NULL DEFAULT '',
    version      VARCHAR(20)  NOT NULL DEFAULT '',
    capabilities JSONB        NOT NULL DEFAULT '{}'::jsonb,
    last_seen    TIMESTAMPTZ,
    is_active    BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Учётные записи облаков производителей (Deye Cloud OpenAPI)
CREATE TABLE vendor_accounts (
    id              BIGSERIAL PRIMARY KEY,
    vendor          VARCHAR(30)  NOT NULL DEFAULT 'deye',
    name            VARCHAR(100) NOT NULL,
    base_url        VARCHAR(200) NOT NULL DEFAULT 'https://eu1-developer.deyecloud.com',
    app_id          VARCHAR(100) NOT NULL,
    app_secret      VARCHAR(255) NOT NULL,
    email           VARCHAR(254) NOT NULL DEFAULT '',
    username        VARCHAR(150) NOT NULL DEFAULT '',
    mobile          VARCHAR(30)  NOT NULL DEFAULT '',
    country_code    VARCHAR(8)   NOT NULL DEFAULT '',
    password_sha256 VARCHAR(64)  NOT NULL,
    company_id      BIGINT,
    is_active       BOOLEAN      NOT NULL DEFAULT TRUE,
    last_error      TEXT         NOT NULL DEFAULT '',
    last_sync_at    TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Модем инвертора (SN под QR-кодом). Через него сервер видит всё оборудование станции.
CREATE TABLE modems (
    id            BIGSERIAL PRIMARY KEY,
    client_id     BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    serial        VARCHAR(100) UNIQUE NOT NULL,
    name          VARCHAR(100) NOT NULL DEFAULT '',
    has_generator BOOLEAN      NOT NULL DEFAULT FALSE,
    site          VARCHAR(200) NOT NULL DEFAULT '',
    agent_id      BIGINT REFERENCES agents(id) ON DELETE SET NULL,
    connection    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    sections      JSONB        NOT NULL DEFAULT '[]'::jsonb,
    status        VARCHAR(10)  NOT NULL DEFAULT 'offline',
    last_seen     TIMESTAMPTZ,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_modems_client ON modems(client_id);
CREATE INDEX idx_modems_status ON modems(status);
CREATE INDEX idx_modems_protocol ON modems((connection->>'protocol'));

CREATE TABLE devices (
    id            BIGSERIAL PRIMARY KEY,
    client_id     BIGINT NOT NULL REFERENCES clients(id) ON DELETE CASCADE,
    modem_id      BIGINT REFERENCES modems(id) ON DELETE CASCADE,
    device_type   VARCHAR(20)  NOT NULL,
    name          VARCHAR(100) NOT NULL DEFAULT '',
    serial_number VARCHAR(100) UNIQUE NOT NULL,
    device_id     VARCHAR(100) UNIQUE NOT NULL,
    status        VARCHAR(10)  NOT NULL DEFAULT 'offline',
    last_seen     TIMESTAMPTZ,
    -- Последние показания храним прямо здесь, чтобы списки открывались быстро
    last_data     JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_devices_client ON devices(client_id);
CREATE INDEX idx_devices_modem ON devices(modem_id);
CREATE INDEX idx_devices_status ON devices(status);

-- Вся история показаний. Индекс (device, timestamp) — для графиков.
CREATE TABLE telemetry (
    id              BIGSERIAL PRIMARY KEY,
    device_id       BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    timestamp       TIMESTAMPTZ NOT NULL,
    current         DOUBLE PRECISION,
    voltage         DOUBLE PRECISION,
    power           DOUBLE PRECISION,
    battery_percent DOUBLE PRECISION,
    temperature     DOUBLE PRECISION,
    error           VARCHAR(255)
);

CREATE INDEX telemetry_device_time ON telemetry(device_id, timestamp);
CREATE INDEX idx_telemetry_time ON telemetry(timestamp DESC);

CREATE TABLE alerts (
    id         BIGSERIAL PRIMARY KEY,
    device_id  BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    kind       VARCHAR(20)  NOT NULL DEFAULT 'error',
    text       VARCHAR(500) NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    is_read    BOOLEAN      NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_alerts_created ON alerts(created_at DESC);
CREATE INDEX idx_alerts_unread ON alerts(is_read);
CREATE INDEX idx_alerts_device ON alerts(device_id);

-- Поиск инверторов: задачи агентов и найденные устройства
CREATE TABLE found_devices (
    id              BIGSERIAL PRIMARY KEY,
    agent_id        BIGINT REFERENCES agents(id) ON DELETE CASCADE,
    task_id         BIGINT,
    source          VARCHAR(20)  NOT NULL DEFAULT 'agent',
    key             VARCHAR(120) NOT NULL,
    connection_type VARCHAR(20)  NOT NULL DEFAULT 'lan',
    name            VARCHAR(200) NOT NULL DEFAULT '',
    manufacturer    VARCHAR(200) NOT NULL DEFAULT '',
    model           VARCHAR(200) NOT NULL DEFAULT '',
    serial          VARCHAR(100) NOT NULL DEFAULT '',
    ip              VARCHAR(64)  NOT NULL DEFAULT '',
    mac             VARCHAR(32)  NOT NULL DEFAULT '',
    bt_address      VARCHAR(120) NOT NULL DEFAULT '',
    rssi            INTEGER,
    device_kind     VARCHAR(20)  NOT NULL DEFAULT 'unknown',
    protocols       JSONB        NOT NULL DEFAULT '[]'::jsonb,
    details         JSONB        NOT NULL DEFAULT '{}'::jsonb,
    status          VARCHAR(20)  NOT NULL DEFAULT 'found',
    check_result    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    modem_id        BIGINT REFERENCES modems(id) ON DELETE SET NULL,
    first_seen      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_seen       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- agent_id может быть NULL (найдено браузером) — уникальность через COALESCE
CREATE UNIQUE INDEX found_device_agent_key ON found_devices(COALESCE(agent_id, 0), key);
CREATE INDEX idx_found_devices_last_seen ON found_devices(last_seen DESC);

CREATE TABLE agent_tasks (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    BIGINT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    kind        VARCHAR(20) NOT NULL,
    method      VARCHAR(20) NOT NULL DEFAULT '',
    params      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    device_id   BIGINT REFERENCES found_devices(id) ON DELETE CASCADE,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    result      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    error       TEXT        NOT NULL DEFAULT '',
    log         TEXT        NOT NULL DEFAULT '',
    created_by  BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at  TIMESTAMPTZ,
    finished_at TIMESTAMPTZ
);

CREATE INDEX idx_agent_tasks_agent_status ON agent_tasks(agent_id, status);

ALTER TABLE found_devices
    ADD CONSTRAINT found_devices_task_fk FOREIGN KEY (task_id) REFERENCES agent_tasks(id) ON DELETE SET NULL;
