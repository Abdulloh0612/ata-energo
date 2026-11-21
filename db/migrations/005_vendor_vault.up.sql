ALTER TABLE vendor_accounts ADD COLUMN fields_encrypted BYTEA, ADD COLUMN key_version TEXT;
ALTER TABLE vendor_accounts ADD CONSTRAINT vendor_credentials_envelope CHECK
    ((fields_encrypted IS NULL AND key_version IS NULL) OR
     (fields_encrypted IS NOT NULL AND key_version IS NOT NULL AND app_id='' AND app_secret=''
      AND email='' AND username='' AND mobile='' AND country_code='' AND password_sha256='' AND company_id IS NULL));
UPDATE vendor_accounts SET last_error='' WHERE last_error<>'';
ALTER TABLE vendor_accounts ADD CONSTRAINT vendor_credentials_encrypted CHECK (fields_encrypted IS NOT NULL) NOT VALID;
