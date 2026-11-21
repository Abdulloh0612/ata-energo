CREATE TABLE provider_credential_events (
    id BIGSERIAL PRIMARY KEY,
    account_id BIGINT NOT NULL,
    actor_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    provider_code TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('created','updated','deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE FUNCTION audit_provider_credential() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    actor BIGINT := NULLIF(current_setting('ata.provider_actor',true),'')::bigint;
BEGIN
    IF TG_OP='DELETE' THEN
        INSERT INTO provider_credential_events(account_id,actor_id,provider_code,action) VALUES(OLD.id,actor,OLD.vendor,'deleted');
        RETURN OLD;
    END IF;
    INSERT INTO provider_credential_events(account_id,actor_id,provider_code,action)
    VALUES(NEW.id,actor,NEW.vendor,CASE WHEN TG_OP='INSERT' THEN 'created' ELSE 'updated' END);
    RETURN NEW;
END;
$$;
CREATE TRIGGER provider_credential_changed AFTER INSERT OR UPDATE OF fields_encrypted,is_active OR DELETE
ON vendor_accounts FOR EACH ROW EXECUTE FUNCTION audit_provider_credential();
