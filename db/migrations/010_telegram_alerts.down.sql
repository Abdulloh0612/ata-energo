DROP TRIGGER telegram_work_change ON crm_work;
DROP FUNCTION enqueue_telegram_work();
DROP TRIGGER telegram_alert_insert ON alerts;
DROP FUNCTION enqueue_telegram_alert();
DROP TABLE telegram_deliveries;
DROP TABLE alert_responses;
ALTER TABLE alerts DROP COLUMN response_status, DROP COLUMN response_user_id;
