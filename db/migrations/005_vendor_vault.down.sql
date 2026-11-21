DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM vendor_accounts WHERE fields_encrypted IS NOT NULL) THEN
        RAISE EXCEPTION 'Remove encrypted credentials before rolling back the vault schema';
    END IF;
END $$;
ALTER TABLE vendor_accounts DROP CONSTRAINT vendor_credentials_encrypted, DROP CONSTRAINT vendor_credentials_envelope, DROP COLUMN fields_encrypted, DROP COLUMN key_version;
