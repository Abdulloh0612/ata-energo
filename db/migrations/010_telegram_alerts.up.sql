ALTER TABLE alerts
    ADD COLUMN response_status TEXT NOT NULL DEFAULT 'open' CHECK (response_status IN ('open','accepted','onsite','resolved')),
    ADD COLUMN response_user_id BIGINT REFERENCES users(id) ON DELETE SET NULL;

CREATE TABLE alert_responses (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL REFERENCES alerts(id) ON DELETE CASCADE,
    actor_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    status TEXT NOT NULL CHECK (status IN ('accepted','onsite','resolved')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE telegram_deliveries (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    chat_id BIGINT NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('incidents','work','inventory','attendance')),
    alert_id BIGINT REFERENCES alerts(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    path TEXT NOT NULL,
    priority SMALLINT NOT NULL DEFAULT 0,
    message_id BIGINT,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt TIMESTAMPTZ NOT NULL DEFAULT now(),
    pending BOOLEAN NOT NULL DEFAULT true,
    last_error TEXT,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    UNIQUE (alert_id, user_id)
);
CREATE INDEX telegram_deliveries_pending_idx ON telegram_deliveries (priority DESC, next_attempt, id) WHERE pending;

CREATE FUNCTION enqueue_telegram_alert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.kind IN ('error', 'offline') THEN
        INSERT INTO telegram_deliveries (user_id,chat_id,category,alert_id,title,path,priority)
        SELECT u.id,u.telegram_id,'incidents',NEW.id,'🚨 ' || c.name || ' — ' || NEW.text,'/alerts/' || NEW.id,10
        FROM devices d JOIN clients c ON c.id=d.client_id CROSS JOIN users u
        WHERE d.id=NEW.device_id AND u.is_active AND u.telegram_id IS NOT NULL AND u.notifications_enabled
          AND u.notification_preferences->'incidents'='true'::jsonb
          AND (u.role IN ('admin','it_head') OR (u.role='technician' AND EXISTS (
            SELECT 1 FROM crm_work w WHERE w.assignee_id=u.id AND w.client_id=d.client_id AND w.status<>'closed'
          )));
    END IF;
    RETURN NEW;
END;
$$;
CREATE TRIGGER telegram_alert_insert AFTER INSERT ON alerts FOR EACH ROW EXECUTE FUNCTION enqueue_telegram_alert();

CREATE FUNCTION enqueue_telegram_work() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE label TEXT;
BEGIN
    IF TG_OP='INSERT' OR NEW.assignee_id IS DISTINCT FROM OLD.assignee_id THEN
        label := 'Назначено';
    ELSIF NEW.status IS DISTINCT FROM OLD.status THEN
        label := CASE NEW.status WHEN 'closed' THEN 'Закрыто' WHEN 'progress' THEN 'В работе' ELSE 'Открыто' END;
    ELSE
        RETURN NEW;
    END IF;
    INSERT INTO telegram_deliveries (user_id,chat_id,category,title,path,priority)
    SELECT u.id,u.telegram_id,'work','📋 ' || label || ' — ' || NEW.title,'/' || NEW.kind || '?id=' || NEW.id,
           CASE WHEN NEW.priority='high' THEN 5 ELSE 0 END
    FROM users u WHERE u.id=NEW.assignee_id AND u.is_active AND u.telegram_id IS NOT NULL AND u.notifications_enabled
      AND u.notification_preferences->'work'='true'::jsonb;
    RETURN NEW;
END;
$$;
CREATE TRIGGER telegram_work_change AFTER INSERT OR UPDATE ON crm_work FOR EACH ROW EXECUTE FUNCTION enqueue_telegram_work();
