// Package api — REST API сайта (JWT): /api/auth, /api/me, /api/users, /api/clients,
// /api/devices, /api/modems, /api/alerts, /api/agents, /api/discovery, /api/deye.
//
// Формат ответов совместим с Python-версией (Django REST Framework), чтобы фронтенд
// работал без изменений: списки — массивы, ошибки — {"detail": "..."} или {"поле": ["..."]}.
package api

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"ata-energo/services"

	"github.com/gofiber/fiber/v2"
)

// DetailResponse — ошибка для человека.
type DetailResponse struct {
	Detail string `json:"detail" example:"Страница не найдена."`
}

func detail(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(DetailResponse{Detail: msg})
}

func notFound(c *fiber.Ctx) error {
	return detail(c, fiber.StatusNotFound, "Страница не найдена.")
}

// fail превращает ошибку сервиса в HTTP-ответ.
func fail(c *fiber.Ctx, err error) error {
	var fe services.FieldErrors
	if errors.As(err, &fe) {
		return c.Status(fiber.StatusBadRequest).JSON(fe)
	}
	var de *services.DetailError
	if errors.As(err, &de) {
		return detail(c, de.Status, de.Detail)
	}
	return err // 500 — обработает ErrorHandler сервера
}

// bodyMap — тело запроса как JSON-объект (пустое тело = {}).
func bodyMap(c *fiber.Ctx) (map[string]any, error) {
	data := map[string]any{}
	body := c.Body()
	if len(strings.TrimSpace(string(body))) == 0 {
		return data, nil
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}
	return data, nil
}

// parseBody — тело в структуру; ошибка -> 400.
func parseBody(c *fiber.Ctx, out any) error {
	if len(strings.TrimSpace(string(c.Body()))) == 0 {
		return nil
	}
	if err := json.Unmarshal(c.Body(), out); err != nil {
		return detail(c, fiber.StatusBadRequest, "Неверный JSON: "+err.Error())
	}
	return nil
}

func paramID(c *fiber.Ctx, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Params(name), 10, 64)
	return id, err == nil && id > 0
}

func queryInt64(c *fiber.Ctx, name string) *int64 {
	v := c.Query(name)
	if v == "" {
		return nil
	}
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &id
}

func queryIntRange(c *fiber.Ctx, name string, def, lo, hi int) int {
	v, err := strconv.Atoi(c.Query(name))
	if err != nil {
		return def
	}
	return max(lo, min(v, hi))
}

// parseTime — строка из адреса (2026-01-31, 2026-01-31T10:00 или RFC3339) в дату-время.
func parseTime(value string, loc *time.Location) (*time.Time, bool) {
	if value == "" {
		return nil, true
	}
	layouts := []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"}
	for _, l := range layouts {
		var t time.Time
		var err error
		if l == time.RFC3339Nano {
			t, err = time.Parse(l, value)
		} else {
			t, err = time.ParseInLocation(l, value, loc)
		}
		if err == nil {
			return &t, true
		}
	}
	return nil, false
}

// pickBool — значение поля JSON как bool (Python: bool(value)).
func pickBool(data map[string]any, key string) bool {
	switch v := data[key].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v != "" && v != "0" && strings.ToLower(v) != "false"
	}
	return false
}
