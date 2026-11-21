CREATE TABLE crm_equipment (
    device_id BIGINT PRIMARY KEY REFERENCES devices(id) ON DELETE CASCADE,
    model VARCHAR(120) NOT NULL DEFAULT '',
    nominal_watts NUMERIC CHECK (nominal_watts > 0 AND nominal_watts <= 1000000000),
    installed_on DATE,
    serviced_on DATE,
    service_due_on DATE,
    warranty_until DATE,
    maintenance BOOLEAN NOT NULL DEFAULT false
);
CREATE TABLE crm_settings (
    singleton BOOLEAN PRIMARY KEY CHECK (singleton),
    low_power_percent SMALLINT NOT NULL CHECK (low_power_percent BETWEEN 1 AND 100)
);
INSERT INTO crm_settings VALUES (true,80);
