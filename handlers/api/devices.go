package api

import (
	"time"

	"ata-energo/db/repositories"
	"ata-energo/middleware"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

// DeviceHandler — /api/devices/, /api/my/devices/, /api/my/telemetry/, /api/modems/.
type DeviceHandler struct {
	devices     *services.DeviceService
	modems      *services.ModemService
	clientsRepo repositories.ClientRepository
	loc         *time.Location
}

func NewDeviceHandler(devices *services.DeviceService, modems *services.ModemService,
	clientsRepo repositories.ClientRepository, loc *time.Location) *DeviceHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &DeviceHandler{devices: devices, modems: modems, clientsRepo: clientsRepo, loc: loc}
}

// ─── устройства ──────────────────────────────────────────────────────────────

// ListDevices godoc
// @Summary Устройства (каждый видит своё). Фильтры: ?client=ID ?type=inverter ?status=online
// @Tags Devices
// @Security BearerAuth
// @Success 200 {array} types.DeviceView
// @Router /devices/ [get]
func (h *DeviceHandler) ListDevices(c *fiber.Ctx) error {
	list, err := h.devices.List(c.UserContext(), middleware.GetUser(c), types.DeviceFilter{
		ClientID: queryInt64(c, "client"), Type: c.Query("type"), Status: c.Query("status"),
	})
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// MyDevices godoc
// @Summary Устройства текущего клиента (роль user)
// @Tags Devices
// @Security BearerAuth
// @Success 200 {array} types.DeviceView
// @Router /my/devices/ [get]
func (h *DeviceHandler) MyDevices(c *fiber.Ctx) error {
	list, err := h.devices.List(c.UserContext(), middleware.GetUser(c), types.DeviceFilter{})
	if err != nil {
		return err
	}
	out := make([]types.DeviceView, len(list))
	copy(out, list)
	// как order_by("device_type", "device_id")
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && (out[j].DeviceType < out[j-1].DeviceType ||
			(out[j].DeviceType == out[j-1].DeviceType && out[j].DeviceID < out[j-1].DeviceID)); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return c.JSON(out)
}

// GetDevice godoc
// @Summary Устройство (чужое — 404)
// @Tags Devices
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.DeviceView
// @Router /devices/{id}/ [get]
func (h *DeviceHandler) GetDevice(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	d, err := h.devices.Get(c.UserContext(), middleware.GetUser(c), id)
	if err != nil {
		return err
	}
	if d == nil {
		return notFound(c)
	}
	return c.JSON(d)
}

// CreateDevice godoc
// @Summary Создать устройство (admin, it_head)
// @Tags Devices
// @Security BearerAuth
// @Param body body types.DeviceWrite true "Устройство"
// @Success 201 {object} types.DeviceView
// @Router /devices/ [post]
func (h *DeviceHandler) CreateDevice(c *fiber.Ctx) error {
	var w types.DeviceWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	d, err := h.devices.Create(c.UserContext(), w)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(d)
}

// UpdateDevice godoc
// @Summary Изменить устройство (admin, it_head)
// @Tags Devices
// @Security BearerAuth
// @Param id path int true "ID"
// @Param body body types.DeviceWrite true "Поля"
// @Success 200 {object} types.DeviceView
// @Router /devices/{id}/ [patch]
func (h *DeviceHandler) UpdateDevice(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var w types.DeviceWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	d, err := h.devices.Update(c.UserContext(), id, w)
	if err != nil {
		return fail(c, err)
	}
	if d == nil {
		return notFound(c)
	}
	return c.JSON(d)
}

// DeleteDevice godoc
// @Summary Удалить устройство (admin, it_head)
// @Tags Devices
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 204
// @Router /devices/{id}/ [delete]
func (h *DeviceHandler) DeleteDevice(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	d, err := h.devices.Get(c.UserContext(), middleware.GetUser(c), id)
	if err != nil {
		return err
	}
	if d == nil {
		return notFound(c)
	}
	if err := h.devices.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// History godoc
// @Summary История показаний для графиков (?from=&to=&limit=500; по умолчанию 24 ч)
// @Tags Devices
// @Security BearerAuth
// @Param id path int true "ID"
// @Param from query string false "2026-01-31T10:00:00"
// @Param to query string false "2026-01-31T12:00:00"
// @Param limit query int false "10..5000"
// @Success 200 {object} map[string]interface{}
// @Router /devices/{id}/history/ [get]
func (h *DeviceHandler) History(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	d, err := h.devices.Get(c.UserContext(), middleware.GetUser(c), id)
	if err != nil {
		return err
	}
	if d == nil {
		return notFound(c)
	}
	to, okTo := parseTime(c.Query("to"), h.loc)
	if !okTo {
		return c.Status(fiber.StatusBadRequest).JSON(services.Field("to", "Неверный формат. Пример: 2026-01-31T10:00:00"))
	}
	from, okFrom := parseTime(c.Query("from"), h.loc)
	if !okFrom {
		return c.Status(fiber.StatusBadRequest).JSON(services.Field("from", "Неверный формат. Пример: 2026-01-31T10:00:00"))
	}
	if to == nil {
		now := time.Now()
		to = &now
	}
	if from == nil {
		f := to.Add(-24 * time.Hour)
		from = &f
	}
	limit := queryIntRange(c, "limit", 500, 10, 5000)
	res, err := h.devices.History(c.UserContext(), d, *from, *to, limit)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

// MyTelemetry godoc
// @Summary Последние показания своих устройств (роль user). ?limit=100 ?device=ID
// @Tags Devices
// @Security BearerAuth
// @Success 200 {array} types.Telemetry
// @Router /my/telemetry/ [get]
func (h *DeviceHandler) MyTelemetry(c *fiber.Ctx) error {
	limit := queryIntRange(c, "limit", 100, 1, 1000)
	list, err := h.devices.MyTelemetry(c.UserContext(), middleware.GetUser(c), queryInt64(c, "device"), limit)
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// ─── модемы ──────────────────────────────────────────────────────────────────

// ListModems godoc
// @Summary Модемы (каждый видит своё). Фильтры: ?client=ID ?search=SN
// @Tags Modems
// @Security BearerAuth
// @Success 200 {array} types.ModemView
// @Router /modems/ [get]
func (h *DeviceHandler) ListModems(c *fiber.Ctx) error {
	list, err := h.modems.List(c.UserContext(), middleware.GetUser(c), types.ModemFilter{
		ClientID: queryInt64(c, "client"), Search: c.Query("search"),
	})
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// GetModem godoc
// @Summary Модем
// @Tags Modems
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.ModemView
// @Router /modems/{id}/ [get]
func (h *DeviceHandler) GetModem(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	m, err := h.modems.Get(c.UserContext(), middleware.GetUser(c), id)
	if err != nil {
		return err
	}
	if m == nil {
		return notFound(c)
	}
	return c.JSON(m)
}

// CreateModem godoc
// @Summary Создать модем {client, serial, name, has_generator, site} — устройства создаются сами
// @Tags Modems
// @Security BearerAuth
// @Param body body types.ModemWrite true "Модем"
// @Success 201 {object} types.ModemView
// @Router /modems/ [post]
func (h *DeviceHandler) CreateModem(c *fiber.Ctx) error {
	var w types.ModemWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	m, err := h.modems.Create(c.UserContext(), h.clientsRepo, w)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(m)
}

// UpdateModem godoc
// @Summary Изменить модем (смена SN переименует устройства)
// @Tags Modems
// @Security BearerAuth
// @Param id path int true "ID"
// @Param body body types.ModemWrite true "Поля"
// @Success 200 {object} types.ModemView
// @Router /modems/{id}/ [patch]
func (h *DeviceHandler) UpdateModem(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var w types.ModemWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	m, err := h.modems.Update(c.UserContext(), h.clientsRepo, id, w)
	if err != nil {
		return fail(c, err)
	}
	if m == nil {
		return notFound(c)
	}
	return c.JSON(m)
}

// DeleteModem godoc
// @Summary Удалить модем вместе с устройствами
// @Tags Modems
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 204
// @Router /modems/{id}/ [delete]
func (h *DeviceHandler) DeleteModem(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	m, err := h.modems.Get(c.UserContext(), nil, id)
	if err != nil {
		return err
	}
	if m == nil {
		return notFound(c)
	}
	if err := h.modems.Delete(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}
