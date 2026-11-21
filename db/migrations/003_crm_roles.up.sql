CREATE TABLE crm_roles (
    code VARCHAR(20) PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 80),
    system BOOLEAN NOT NULL,
    grants JSONB
);
INSERT INTO crm_roles (code, name, system) VALUES
    ('admin','Администратор',true), ('it_head','Руководитель',true),
    ('manager','Менеджер',true), ('technician','Техник',true),
    ('observer','Наблюдатель',true), ('dispatcher','Диспетчер',true),
    ('user','Клиент',true);
ALTER TABLE users ADD CONSTRAINT users_crm_role_fkey FOREIGN KEY (role) REFERENCES crm_roles(code) NOT VALID;
