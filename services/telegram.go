package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ata-energo/pkg/telegram"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

type Telegram struct {
	pool                    *pgxpool.Pool
	auth                    *AuthService
	client                  http.Client
	token, username, appURL string
}

func NewTelegram(pool *pgxpool.Pool, auth *AuthService, token, username, appURL string) (*Telegram, error) {
	if token == "" {
		return nil, nil
	}
	u, err := url.Parse(appURL)
	if (appURL != "" && (err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/"))) ||
		!regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]+$`).MatchString(token) ||
		!regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`).MatchString(username) {
		return nil, errors.New("invalid Telegram bot configuration")
	}
	return &Telegram{pool: pool, auth: auth, client: http.Client{
		Timeout:       35 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}, token: token, username: username, appURL: strings.TrimRight(appURL, "/")}, nil
}

func (s *Telegram) Login(ctx context.Context, raw string) (*LoginResult, error) {
	proof, err := telegram.VerifyInitData(raw, s.token, time.Now())
	if err != nil {
		return nil, Detail(401, "Сессия Telegram истекла. Откройте приложение заново.")
	}
	var id int64
	err = s.pool.QueryRow(ctx, `SELECT id FROM users WHERE telegram_id=$1 AND is_active`, proof.User.ID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Detail(403, "Привяжите Telegram в профиле CRM.")
	}
	if err != nil {
		return nil, err
	}
	user, err := s.auth.users.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive {
		return nil, Detail(403, "Учётная запись недоступна.")
	}
	result, err := s.auth.loginUser(ctx, user)
	if err != nil {
		return nil, err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO telegram_login_proofs VALUES ($1,$2) ON CONFLICT DO NOTHING`, proof.Digest, proof.ExpiresAt)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() != 1 {
		return nil, Detail(401, "Откройте Mini App заново для входа.")
	}
	return result, nil
}

type TelegramPreferences struct {
	Linked     bool            `json:"linked"`
	Enabled    *bool           `json:"notifications_enabled"`
	Categories map[string]bool `json:"notification_preferences"`
}

func (s *Telegram) Linked(ctx context.Context, userID int64) (TelegramPreferences, error) {
	var prefs TelegramPreferences
	err := s.pool.QueryRow(ctx, `SELECT telegram_id IS NOT NULL,notifications_enabled,notification_preferences FROM users WHERE id=$1`,
		userID).Scan(&prefs.Linked, &prefs.Enabled, &prefs.Categories)
	return prefs, err
}

func (s *Telegram) SetPreferences(ctx context.Context, userID int64, prefs TelegramPreferences) (TelegramPreferences, error) {
	for category := range prefs.Categories {
		switch category {
		case "incidents", "work", "inventory", "attendance":
		default:
			return TelegramPreferences{}, Detail(400, "Неизвестная категория уведомлений.")
		}
	}
	if prefs.Categories == nil {
		prefs.Categories = map[string]bool{}
	}
	err := s.pool.QueryRow(ctx, `UPDATE users SET notifications_enabled=coalesce($2,notifications_enabled),
		notification_preferences=notification_preferences || $3::jsonb WHERE id=$1 AND telegram_id IS NOT NULL
		RETURNING true,notifications_enabled,notification_preferences`,
		userID, prefs.Enabled, prefs.Categories).Scan(&prefs.Linked, &prefs.Enabled, &prefs.Categories)
	if errors.Is(err, pgx.ErrNoRows) {
		return TelegramPreferences{}, Detail(409, "Сначала привяжите Telegram.")
	}
	return prefs, err
}

func (s *Telegram) Link(ctx context.Context, userID int64) (string, error) {
	var secret [24]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(secret[:])
	digest := sha256.Sum256([]byte(code))
	tag, err := s.pool.Exec(ctx, `INSERT INTO telegram_link_codes (user_id,digest,expires_at)
		SELECT id,$2,now()+interval '10 minutes' FROM users WHERE id=$1 AND is_active AND telegram_id IS NULL
		ON CONFLICT (user_id) DO UPDATE SET digest=excluded.digest, expires_at=excluded.expires_at`, userID, digest[:])
	if err != nil {
		return "", err
	}
	if tag.RowsAffected() != 1 {
		return "", Detail(409, "Telegram уже привязан к аккаунту.")
	}
	return "https://t.me/" + s.username + "?start=" + code, nil
}

func (s *Telegram) bind(ctx context.Context, code string, user telegram.Identity) (bool, error) {
	if len(code) != 32 || user.ID <= 0 {
		return false, nil
	}
	digest := sha256.Sum256([]byte(code))
	tag, err := s.pool.Exec(ctx, `WITH link AS (
		DELETE FROM telegram_link_codes WHERE digest=$1 AND expires_at>now() RETURNING user_id
	)
	UPDATE users SET telegram_id=$2, telegram_username=NULLIF($3,''), notifications_enabled=true
	FROM link WHERE users.id=link.user_id AND users.is_active AND (users.telegram_id IS NULL OR users.telegram_id=$2)`,
		digest[:], user.ID, user.Username)
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" {
		return false, nil
	}
	return tag.RowsAffected() == 1, err
}

type telegramUpdate struct {
	ID       int64 `json:"update_id"`
	Callback *struct {
		ID   string            `json:"id"`
		From telegram.Identity `json:"from"`
		Data string            `json:"data"`
	} `json:"callback_query"`
	Message *struct {
		From telegram.Identity `json:"from"`
		Text string            `json:"text"`
		Chat struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
	} `json:"message"`
}

func (s *Telegram) call(ctx context.Context, method string, data, result any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.telegram.org/bot"+s.token+"/"+method, bytes.NewReader(payload))
	if err != nil {
		return errors.New("could not create Telegram request")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return errors.New("Telegram API unavailable")
	}
	defer res.Body.Close()
	reply := struct {
		ErrorCode   int    `json:"error_code"`
		Description string `json:"description"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
		OK     bool `json:"ok"`
		Result any  `json:"result"`
	}{Result: result}
	if err := json.NewDecoder(res.Body).Decode(&reply); err != nil || !reply.OK || res.StatusCode != http.StatusOK {
		code := reply.ErrorCode
		if code == 0 {
			code = res.StatusCode
		}
		return &telegramAPIError{Code: code, RetryAfter: reply.Parameters.RetryAfter, Unchanged: strings.Contains(reply.Description, "message is not modified")}
	}
	return nil
}

func (s *Telegram) Run(ctx context.Context, log *zap.Logger) {
	var offset int64
	var cleanup, lastPollError time.Time
	connected := false
	for ctx.Err() == nil {
		if time.Since(cleanup) > time.Hour {
			_, _ = s.pool.Exec(ctx, `DELETE FROM telegram_login_proofs WHERE expires_at < now()`)
			_, _ = s.pool.Exec(ctx, `DELETE FROM telegram_link_codes WHERE expires_at < now()`)
			cleanup = time.Now()
		}
		var updates []telegramUpdate
		err := s.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 25, "allowed_updates": []string{"message", "callback_query"}}, &updates)
		if err == nil {
			if !connected {
				log.Info("Telegram polling connected", zap.String("bot", s.username))
				connected = true
			}
			for _, update := range updates {
				if err = s.handleUpdate(ctx, update); err != nil {
					break
				}
				offset = update.ID + 1
			}
		}
		if err != nil {
			if ctx.Err() == nil && time.Since(lastPollError) >= time.Minute {
				log.Warn("Telegram polling failed", zap.String("bot", s.username))
				lastPollError = time.Now()
			}
			select {
			case <-ctx.Done():
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func (s *Telegram) handleUpdate(ctx context.Context, update telegramUpdate) error {
	if callback := update.Callback; callback != nil {
		parts := strings.Split(callback.Data, ":")
		if len(parts) != 3 || parts[0] != "a" {
			return nil
		}
		alertID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || alertID <= 0 {
			return nil
		}
		var userID int64
		err = s.pool.QueryRow(ctx, `SELECT id FROM users WHERE telegram_id=$1 AND is_active`, callback.From.ID).Scan(&userID)
		if err == nil {
			err = s.ChangeIncident(ctx, userID, alertID, parts[2])
		}
		text := "Статус обновлён."
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			text = "Привяжите аккаунт CRM."
		case err != nil:
			var detail *DetailError
			if !errors.As(err, &detail) {
				return err
			}
			text = detail.Detail
		}
		_ = s.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callback.ID, "text": text}, nil)
		return nil
	}
	msg := update.Message
	if msg == nil || msg.Chat.Type != "private" {
		return nil
	}
	args := strings.Fields(msg.Text)
	if len(args) == 0 || (args[0] != "/start" && !strings.EqualFold(args[0], "/start@"+s.username)) {
		return nil
	}
	linked := false
	if len(args) == 2 {
		var err error
		linked, err = s.bind(ctx, args[1], msg.From)
		if err != nil {
			return err
		}
	}
	if !linked {
		err := s.pool.QueryRow(ctx, `SELECT EXISTS (
			SELECT 1 FROM users WHERE telegram_id=$1 AND is_active
		)`, msg.From.ID).Scan(&linked)
		if err != nil {
			return err
		}
	}
	text := "Привяжите Telegram в профиле CRM."
	switch {
	case linked && s.appURL == "":
		text = "Telegram привязан к аккаунту CRM. Mini App появится здесь после подключения HTTPS-адреса CRM."
	case linked:
		text = "Telegram привязан к аккаунту CRM."
	case len(args) == 2:
		text = "Ссылка недействительна или уже использована. Создайте новую ссылку в профиле CRM."
	}
	data := map[string]any{"chat_id": msg.Chat.ID, "text": text}
	if keyboard := s.appKeyboard("/"); len(keyboard) > 0 {
		data["reply_markup"] = map[string]any{"inline_keyboard": keyboard}
	}
	_ = s.call(ctx, "sendMessage", data, nil)
	return nil
}

func (s *Telegram) appKeyboard(path string) [][]any {
	if s.appURL == "" {
		return [][]any{}
	}
	return [][]any{{map[string]any{"text": "Открыть в CRM", "web_app": map[string]string{"url": s.appURL + path}}}}
}
