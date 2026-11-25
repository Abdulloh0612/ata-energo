package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cgi"
	"os"
	"os/exec"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	"go.uber.org/zap"
)

const hybridDoctorPath = "/hybrid-doctor"
const hybridDoctorScript = "hybrid-doctor/app.py"

func runHybridDoctorCommand(ctx context.Context, action string) error {
	cmd := exec.CommandContext(ctx, "python3", "-B", hybridDoctorScript, action)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func registerHybridDoctor(app *fiber.App) error {
	python, err := exec.LookPath("python3")
	if err != nil {
		return fmt.Errorf("автосервис: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := runHybridDoctorCommand(ctx, "init"); err != nil {
		return fmt.Errorf("инициализация базы автосервиса: %w", err)
	}

	cgiHandler := &cgi.Handler{
		Path:       python,
		Dir:        ".",
		Root:       hybridDoctorPath,
		Args:       []string{"-B", hybridDoctorScript},
		InheritEnv: []string{"HD_DATA", "HD_SECURE_COOKIE", "LANG", "TZ"},
	}
	serve := adaptor.HTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fiber уже прочитал тело. CGI передаёт его через stdin с CONTENT_LENGTH.
		r.TransferEncoding = nil
		cgiHandler.ServeHTTP(w, r)
	}))
	app.All(hybridDoctorPath, func(c *fiber.Ctx) error {
		if c.Path() == hybridDoctorPath {
			target := hybridDoctorPath + "/"
			if query := c.Request().URI().QueryString(); len(query) > 0 {
				target += "?" + string(query)
			}
			return c.Redirect(target, fiber.StatusPermanentRedirect)
		}
		return serve(c)
	})
	app.All(hybridDoctorPath+"/*", serve)
	return nil
}

func runHybridDoctorBackups(ctx context.Context, logger *zap.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := runHybridDoctorCommand(ctx, "backup-daily"); err != nil && ctx.Err() == nil {
			logger.Warn("Ошибка резервного копирования базы автосервиса", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
