ALTER TABLE users DROP CONSTRAINT users_crm_role_fkey;
UPDATE users SET role='dispatcher' WHERE role IN (SELECT code FROM crm_roles WHERE NOT system);
DROP TABLE crm_roles;
