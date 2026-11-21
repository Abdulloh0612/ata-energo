// Package agent — API для локального агента (agent/ata_agent.py). Вход по заголовку X-Agent-Token.
//
//	POST /api/agent/hello/                  агент сообщает о себе
//	GET  /api/agent/poll/                   забрать задачи и список устройств для опроса
//	POST /api/agent/tasks/{id}/progress/    промежуточный результат {log, found: [...]}
//	POST /api/agent/tasks/{id}/finish/      итог {status, error, result, found: [...]}
//	POST /api/agent/telemetry/              показания {serial, payload} -> обычный мониторинг
package agent

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"ata-energo/middleware"
	"ata-energo/services"

	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	discovery *services.DiscoveryService
}

func NewHandler(discovery *services.DiscoveryService) *Handler {
	return &Handler{discovery: discovery}
}

func body(c *fiber.Ctx) (map[string]any, error) {
	data := map[string]any{}
	if len(strings.TrimSpace(string(c.Body()))) == 0 {
		return data, nil
	}
	err := json.Unmarshal(c.Body(), &data)
	return data, err
}

func fail(c *fiber.Ctx, err error) error {
	var de *services.DetailError
	if errors.As(err, &de) {
		return c.Status(de.Status).JSON(fiber.Map{"detail": de.Detail})
	}
	return err
}

func badJSON(c *fiber.Ctx) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"detail": "Неверный JSON"})
}

// Hello godoc
// @Summary Агент сообщает о себе (hostname, platform, version, local_ip, capabilities)
// @Tags Agent
// @Security AgentToken
// @Param body body map[string]interface{} false "Сведения об агенте"
// @Success 200 {object} map[string]interface{}
// @Router /agent/hello/ [post]
func (h *Handler) Hello(c *fiber.Ctx) error {
	data, err := body(c)
	if err != nil {
		return badJSON(c)
	}
	a := middleware.GetAgent(c)
	if err := h.discovery.Hello(c.UserContext(), a, data); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"agent_id": a.ID, "name": a.Name})
}

// Poll godoc
// @Summary Новые задачи и устройства, которые надо опрашивать
// @Tags Agent
// @Security AgentToken
// @Success 200 {object} map[string]interface{}
// @Router /agent/poll/ [get]
func (h *Handler) Poll(c *fiber.Ctx) error {
	res, err := h.discovery.Poll(c.UserContext(), middleware.GetAgent(c))
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func taskID(c *fiber.Ctx) (int64, bool) {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	return id, err == nil
}

// Progress godoc
// @Summary Промежуточный результат задачи {log, found}
// @Tags Agent
// @Security AgentToken
// @Param id path int true "ID задачи"
// @Param body body map[string]interface{} false "Промежуточные результаты"
// @Success 200 {object} map[string]bool
// @Router /agent/tasks/{id}/progress/ [post]
func (h *Handler) Progress(c *fiber.Ctx) error {
	id, ok := taskID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"detail": "Страница не найдена."})
	}
	data, err := body(c)
	if err != nil {
		return badJSON(c)
	}
	if err := h.discovery.TaskProgress(c.UserContext(), middleware.GetAgent(c), id, data); err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// Finish godoc
// @Summary Итог задачи {status, error, result, found}
// @Tags Agent
// @Security AgentToken
// @Param id path int true "ID задачи"
// @Param body body map[string]interface{} true "Итог задачи"
// @Success 200 {object} map[string]bool
// @Router /agent/tasks/{id}/finish/ [post]
func (h *Handler) Finish(c *fiber.Ctx) error {
	id, ok := taskID(c)
	if !ok {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"detail": "Страница не найдена."})
	}
	data, err := body(c)
	if err != nil {
		return badJSON(c)
	}
	if err := h.discovery.TaskFinish(c.UserContext(), middleware.GetAgent(c), id, data); err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

// Telemetry godoc
// @Summary Показания, прочитанные агентом {serial, payload} -> обычный мониторинг
// @Tags Agent
// @Security AgentToken
// @Param body body map[string]interface{} true "Серийный номер и показания"
// @Success 200 {object} map[string]bool
// @Router /agent/telemetry/ [post]
func (h *Handler) Telemetry(c *fiber.Ctx) error {
	data, err := body(c)
	if err != nil {
		return badJSON(c)
	}
	if err := h.discovery.AgentTelemetry(c.UserContext(), middleware.GetAgent(c), data); err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"ok": true})
}
