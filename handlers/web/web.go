// Package web — отдаёт одну HTML-страницу сайта. Всё остальное делает JavaScript через API.
package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
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
	assets, err := readFrontendAssets(webDir)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, struct {
		ShowDemo bool
		Frontend frontendAssets
	}{showDemo, assets}); err != nil {
		return nil, err
	}
	return &Handler{page: buf.Bytes()}, nil
}

type frontendAssets struct {
	File string   `json:"file"`
	CSS  []string `json:"css"`
}

func readFrontendAssets(webDir string) (frontendAssets, error) {
	path := filepath.Join(webDir, "static", "build", ".vite", "manifest.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return frontendAssets{}, nil // исходный интерфейс при запуске Go без сборки Vite
	}
	if err != nil {
		return frontendAssets{}, err
	}
	var manifest map[string]frontendAssets
	if err := json.Unmarshal(data, &manifest); err != nil {
		return frontendAssets{}, fmt.Errorf("read Vite manifest: %w", err)
	}
	assets := manifest["frontend/main.ts"]
	if assets.File == "" {
		return frontendAssets{}, fmt.Errorf("Vite manifest is missing frontend/main.ts")
	}
	return assets, nil
}

// Index — главная страница сайта (одностраничное приложение).
func (h *Handler) Index(c *fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderCacheControl, "no-cache")
	return c.Send(h.page)
}
