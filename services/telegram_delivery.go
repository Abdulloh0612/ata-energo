package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type telegramAPIError struct {
	Code       int
	RetryAfter int
	Unchanged  bool
}

func (e *telegramAPIError) Error() string {
	return fmt.Sprintf("Telegram API error %d", e.Code)
}

func (s *Telegram) RunDelivery(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := s.deliverNext(ctx)
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (s *Telegram) deliverNext(ctx context.Context) (bool, error) {
	var id, chat, alert, message, revision int64
	var title, path, status, actor string
	var attempts int
	var eligible bool
	err := s.pool.QueryRow(ctx, `WITH candidate AS (
		SELECT id FROM telegram_deliveries WHERE pending AND next_attempt<=now()
		ORDER BY priority DESC,next_attempt,id LIMIT 1 FOR UPDATE SKIP LOCKED
	), claimed AS (
		UPDATE telegram_deliveries d SET next_attempt=now()+interval '1 minute'
		FROM candidate WHERE d.id=candidate.id RETURNING d.*
	)
	SELECT d.id,d.chat_id,coalesce(d.alert_id,0),coalesce(d.message_id,0),d.revision,d.title,d.path,d.attempts,
		coalesce(a.response_status,''),coalesce(nullif(trim(concat_ws(' ',r.first_name,r.last_name)),''),r.username,''),
		coalesce(u.is_active AND u.telegram_id=d.chat_id AND u.notifications_enabled
		  AND u.notification_preferences->d.category='true'::jsonb,false)
	FROM claimed d JOIN users u ON u.id=d.user_id LEFT JOIN alerts a ON a.id=d.alert_id
	LEFT JOIN users r ON r.id=a.response_user_id`).Scan(&id, &chat, &alert, &message, &revision, &title, &path, &attempts, &status, &actor, &eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !eligible {
		_, err = s.pool.Exec(ctx, `UPDATE telegram_deliveries SET pending=false,last_error='recipient disabled' WHERE id=$1 AND revision=$2`, id, revision)
		return true, err
	}

	keyboard := s.appKeyboard(path)
	if alert != 0 {
		labels := map[string]string{"open": "Открыта", "accepted": "Принята в работу", "onsite": "На объекте", "resolved": "Устранена"}
		title += "\n" + labels[status]
		if actor != "" {
			title += " · " + actor
		}
		if next, label := nextIncidentAction(status); next != "" {
			keyboard = append(keyboard, []any{map[string]string{"text": label, "callback_data": fmt.Sprintf("a:%d:%s", alert, next)}})
		}
	}
	data := map[string]any{"chat_id": chat, "text": title, "reply_markup": map[string]any{"inline_keyboard": keyboard}}
	var sent struct {
		ID int64 `json:"message_id"`
	}
	method := "sendMessage"
	if message != 0 {
		method = "editMessageText"
		data["message_id"] = message
	}
	sendErr := s.call(ctx, method, data, &sent)
	var apiErr *telegramAPIError
	_ = errors.As(sendErr, &apiErr)
	if sendErr == nil || (message != 0 && apiErr != nil && apiErr.Unchanged) {
		if sent.ID != 0 {
			message = sent.ID
		}
		_, err = s.pool.Exec(ctx, `UPDATE telegram_deliveries SET message_id=$2,sent_at=now(),last_error=NULL,
			pending=revision<>$3,next_attempt=now(),attempts=0 WHERE id=$1`, id, message, revision)
		return true, err
	}

	delay := 1 << min(10, attempts+1)
	retry := attempts < 9
	if apiErr != nil {
		if apiErr.Code == http.StatusTooManyRequests {
			delay = max(delay, apiErr.RetryAfter)
			retry = true
			// A bot-wide rate limit also applies to the next recipient.
			_, _ = s.pool.Exec(ctx, `UPDATE telegram_deliveries SET next_attempt=greatest(next_attempt,now()+make_interval(secs=>$1::int)) WHERE pending`, delay)
		} else if apiErr.Code >= 400 && apiErr.Code < 500 {
			retry = false
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE telegram_deliveries SET attempts=attempts+1,last_error=$2,
		pending=$3,next_attempt=now()+make_interval(secs=>$4::int) WHERE id=$1 AND revision=$5`,
		id, sendErr.Error(), retry, delay, revision)
	return true, err
}

func nextIncidentAction(status string) (string, string) {
	switch status {
	case "open":
		return "accepted", "✅ Принял в работу"
	case "accepted":
		return "onsite", "🛠 На объекте"
	case "onsite":
		return "resolved", "✔️ Устранено"
	default:
		return "", ""
	}
}

func (s *Telegram) ChangeIncident(ctx context.Context, userID, alertID int64, target string) error {
	previous := map[string]string{"accepted": "open", "onsite": "accepted", "resolved": "onsite"}[target]
	if previous == "" {
		return Detail(400, "Неизвестный статус аварии.")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `UPDATE alerts a SET response_status=$3,
		response_user_id=CASE WHEN $3='accepted' THEN u.id ELSE a.response_user_id END
		FROM users u, devices d WHERE a.id=$1 AND u.id=$2 AND u.is_active AND d.id=a.device_id
		AND a.response_status=$4 AND (
			u.role IN ('admin','it_head') OR (u.role='technician' AND (
				a.response_user_id=u.id OR ($3='accepted' AND EXISTS (
					SELECT 1 FROM crm_work w WHERE w.assignee_id=u.id AND w.client_id=d.client_id AND w.status<>'closed'
				))
			))
		) RETURNING a.id`, alertID, userID, target, previous).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail(409, "Действие недоступно. Проверьте текущий статус аварии.")
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO alert_responses (alert_id,actor_id,status) VALUES ($1,$2,$3)`, id, userID, target); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE telegram_deliveries SET pending=true,attempts=0,revision=revision+1,next_attempt=now()
		WHERE alert_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
