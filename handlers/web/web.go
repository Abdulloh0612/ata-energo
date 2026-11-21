// Package web — отдаёт одну HTML-страницу сайта. Всё остальное делает JavaScript через API.
package web

import (
	"bytes"
	"html/template"
	"path/filepath"

	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	page []byte
}

// NewHandler читает web/templates/index.html один раз при старте.
// showDemo — показывать подсказку с тестовыми логинами (только при APP_ENV != production).
func NewHandler(webDir string, showDemo bool) (*Handler, error) {
	tpl, err := template.ParseFiles(filepath.Join(webDir, "templates", "index.html"))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, map[string]any{"ShowDemo": showDemo}); err != nil {
		return nil, err
	}
	return &Handler{page: buf.Bytes()}, nil
}

// Index — главная страница сайта (одностраничное приложение).
func (h *Handler) Index(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderCacheControl, "no-cache")
	return c.Send(h.page)
}
