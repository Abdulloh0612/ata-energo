package api

import (
	"ata-energo/db/repositories"
	"ata-energo/middleware"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

// AlertHandler — /api/alerts/ (каждый видит доступные ему; отмечать прочитанными — сотрудники).
type AlertHandler struct {
	alerts repositories.AlertRepository
}

func NewAlertHandler(alerts repositories.AlertRepository) *AlertHandler {
	return &AlertHandler{alerts: alerts}
}

// List godoc
// @Summary Аварии (200 последних). ?unread=1&client=ID
// @Tags Alerts
// @Security BearerAuth
// @Success 200 {array} types.Alert
// @Router /alerts/ [get]
func (h *AlertHandler) List(c *fiber.Ctx) error {
	list, err := h.alerts.List(c.UserContext(), types.AlertFilter{
		OwnerUserID: services.OwnerScope(middleware.GetUser(c)),
		Unread:      c.Query("unread") != "",
		ClientID:    queryInt64(c, "client"),
		Limit:       200,
	})
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// Get godoc
// @Summary Авария
// @Tags Alerts
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.Alert
// @Router /alerts/{id}/ [get]
func (h *AlertHandler) Get(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	a, err := h.alerts.GetByID(c.UserContext(), id, services.OwnerScope(middleware.GetUser(c)))
	if err != nil {
		return err
	}
	if a == nil {
		return notFound(c)
	}
	return c.JSON(a)
}

// Read godoc
// @Summary Отметить аварию прочитанной (сотрудники)
// @Tags Alerts
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.Alert
// @Router /alerts/{id}/read/ [post]
func (h *AlertHandler) Read(c *fiber.Ctx) error {
	if !middleware.GetUser(c).IsStaffViewer() {
		return detail(c, fiber.StatusForbidden, "Только для сотрудников.")
	}
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	a, err := h.alerts.GetByID(c.UserContext(), id, nil)
	if err != nil {
		return err
	}
	if a == nil {
		return notFound(c)
	}
	if err := h.alerts.MarkRead(c.UserContext(), id); err != nil {
		return err
	}
	a.IsRead = true
	return c.JSON(a)
}

// ReadAll godoc
// @Summary Отметить все аварии прочитанными (сотрудники)
// @Tags Alerts
// @Security BearerAuth
// @Success 200 {object} map[string]int
// @Router /alerts/read_all/ [post]
func (h *AlertHandler) ReadAll(c *fiber.Ctx) error {
	if !middleware.GetUser(c).IsStaffViewer() {
		return detail(c, fiber.StatusForbidden, "Только для сотрудников.")
	}
	n, err := h.alerts.MarkAllRead(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"updated": n})
}
