// Package ws — WebSocket: ws://домен/ws/telemetry/?token=<JWT access>
//
// После подключения:
//
//	admin / it_head / dispatcher -> группа "all" (данные всех устройств)
//	user (клиент)                -> группа "client_{id}" (только свои)
//
// Сообщения браузеру (JSON):
//
//	{"type": "hello", "role": "...", "groups": [...]}
//	{"type": "telemetry", "device": {...}, "data": {power, voltage, ...}}
//	{"type": "status", "device": {... "status": "offline"}}
//	{"type": "modem", "modem": {...}}
//	{"type": "alert", "alert": {...}}
//	{"type": "pong"}  (ответ на {"type": "ping"})
//
// Коды закрытия: 4001 — нет/неверный токен, 4003 — клиент без привязки к Client.
package ws

import (
	"context"
	"encoding/json"
	"time"

	"ata-energo/pkg/hub"
	"ata-energo/services"

	"github.com/gofiber/contrib/websocket"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

type Handler struct {
	auth *services.AuthService
	hub  *hub.Hub
	log  *zap.Logger
}

func NewHandler(auth *services.AuthService, h *hub.Hub, log *zap.Logger) *Handler {
	return &Handler{auth: auth, hub: h, log: log}
}

// Upgrade — пропускает только WebSocket-запросы.
func (h *Handler) Upgrade(c *fiber.Ctx) error {
	if websocket.IsWebSocketUpgrade(c) {
		return c.Next()
	}
	return fiber.ErrUpgradeRequired
}

func closeWith(conn *websocket.Conn, code int, text string) {
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, text), time.Now().Add(time.Second))
	_ = conn.Close()
}

// Telemetry — один подключённый браузер.
func (h *Handler) Telemetry() fiber.Handler {
	return websocket.New(func(conn *websocket.Conn) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		u, err := h.auth.UserFromAccess(ctx, conn.Query("token"))
		cancel()
		if err != nil || u == nil {
			closeWith(conn, 4001, "invalid token") // браузер обновит токен и переподключится
			return
		}

		var groups []string
		if u.IsStaffViewer() {
			groups = []string{"all"}
		} else if u.ClientID != nil {
			groups = []string{hub.ClientGroup(*u.ClientID)}
		} else {
			closeWith(conn, 4003, "no client")
			return
		}

		sub := h.hub.Subscribe(groups)
		defer h.hub.Unsubscribe(sub)

		hello, _ := json.Marshal(map[string]any{"type": "hello", "role": u.Role, "groups": groups})
		if err := conn.WriteMessage(websocket.TextMessage, hello); err != nil {
			return
		}

		// чтение: ping -> pong; при закрытии — выходим
		pongs := make(chan struct{}, 4)
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				_, data, err := conn.ReadMessage()
				if err != nil {
					return
				}
				var msg struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(data, &msg) == nil && msg.Type == "ping" {
					select {
					case pongs <- struct{}{}:
					default:
					}
				}
			}
		}()

		pong := []byte(`{"type":"pong"}`)
		for {
			select {
			case <-done:
				return
			case <-pongs:
				if err := conn.WriteMessage(websocket.TextMessage, pong); err != nil {
					return
				}
			case payload := <-sub.C:
				_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
					return
				}
			}
		}
	})
}
