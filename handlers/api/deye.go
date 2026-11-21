package api

import (
	"ata-energo/db/repositories"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"ata-energo/middleware"
	"ata-energo/pkg/deye"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

// DeyeHandler — интеграция с Deye Cloud.
//
// Аккаунты (admin):
//
//	GET/POST          /api/deye/accounts/
//	GET/PATCH/DELETE  /api/deye/accounts/{id}/
//	POST /api/deye/accounts/{id}/test/                    проверить вход (токен + организация)
//	GET  /api/deye/accounts/{id}/stations/                станции (?page&size&with_devices=1&device_type=)
//	GET  /api/deye/accounts/{id}/stations/{sid}/          детали + текущие данные станции
//	GET  /api/deye/accounts/{id}/stations/{sid}/devices/  устройства станции
//	POST /api/deye/accounts/{id}/stations/create/         создать станцию
//	GET  /api/deye/accounts/{id}/devices/                 все устройства (бизнес-аккаунт)
//	POST /api/deye/accounts/{id}/devices/register/        добавить логгер в станцию
//	POST /api/deye/accounts/{id}/loggers/add/             добавить логгеры (бизнес-аккаунт)
//	POST /api/deye/accounts/{id}/loggers/delete/          убрать логгеры (бизнес-аккаунт)
//	POST /api/deye/accounts/{id}/import/                  подключить устройство Deye как модем клиента
//	POST /api/deye/sync/                                  опросить облако прямо сейчас
//
// Модем, подключённый через Deye Cloud (чтение — кому доступен модем, команды — admin):
//
//	GET  /api/modems/{id}/deye/latest/            сырые показания устройства
//	GET  /api/modems/{id}/deye/station/           текущие данные станции
//	GET  /api/modems/{id}/deye/measure-points/    доступные точки измерения
//	POST /api/modems/{id}/deye/history/           история устройства (granularity 1..4)
//	POST /api/modems/{id}/deye/history-raw/       история по точкам за ≤5 дней
//	POST /api/modems/{id}/deye/station-history/   история станции (granularity 1..4)
//	POST /api/modems/{id}/deye/station-history-power/
//	GET  /api/modems/{id}/deye/alerts/            аварии устройства (?from&to — unix или даты, ≤30 дней)
//	GET  /api/modems/{id}/deye/station-alerts/    аварии станции (≤180 дней)
//	GET  /api/modems/{id}/deye/config/            настройки: батарея, режим работы, TOU
//	POST /api/modems/{id}/deye/control/{command}/ команда инвертору
//	POST /api/modems/{id}/deye/dynamic/read/      запросить текущие настройки (dynamicControl)
//	GET  /api/modems/{id}/deye/dynamic/result/{orderId}/
//	GET  /api/modems/{id}/deye/orders/{orderId}/  результат команды (status 666 = выполнено)
type DeyeHandler struct {
	deye      *services.DeyeService
	modems    *services.ModemService
	collector *services.VendorCollector
	loc       *time.Location
}

func NewDeyeHandler(d *services.DeyeService, modems *services.ModemService, collector *services.VendorCollector, loc *time.Location) *DeyeHandler {
	if loc == nil {
		loc = time.UTC
	}
	return &DeyeHandler{deye: d, modems: modems, collector: collector, loc: loc}
}

// deyeFail — ошибки Deye Cloud отдаём как 502 с понятным текстом.
func deyeFail(c *fiber.Ctx, err error) error {
	var ae *deye.APIError
	if errors.As(err, &ae) {
		return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{
			"detail": "Deye Cloud отклонил запрос", "code": ae.Code, "request_id": ae.RequestID,
		})
	}
	var fe services.FieldErrors
	var de *services.DetailError
	if errors.As(err, &fe) || errors.As(err, &de) {
		return fail(c, err)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return detail(c, fiber.StatusGatewayTimeout, "Deye Cloud не ответил вовремя")
	}
	return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"detail": "Не удалось выполнить запрос Deye Cloud"})
}

func pageParams(c *fiber.Ctx, maxSize int) (int, int) {
	return queryIntRange(c, "page", 1, 1, 100000), queryIntRange(c, "size", 20, 1, maxSize)
}

// ─── аккаунты ────────────────────────────────────────────────────────────────

func (h *DeyeHandler) account(c *fiber.Ctx) (*types.VendorAccount, *deye.Client, error) {
	id, ok := paramID(c, "id")
	if !ok {
		return nil, nil, services.Detail(fiber.StatusNotFound, "Страница не найдена.")
	}
	return h.deye.Account(c.UserContext(), id)
}

// ListAccounts godoc
// @Summary Аккаунты Deye Cloud
// @Tags Deye
// @Security BearerAuth
// @Success 200 {array} types.VendorAccount
// @Router /deye/accounts/ [get]
func (h *DeyeHandler) ListAccounts(c *fiber.Ctx) error {
	list, err := h.deye.ListAccounts(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// GetAccount godoc
// @Summary Аккаунт Deye Cloud
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.VendorAccount
// @Router /deye/accounts/{id}/ [get]
func (h *DeyeHandler) GetAccount(c *fiber.Ctx) error {
	a, _, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(a)
}

// CreateAccount godoc
// @Summary Добавить аккаунт Deye: app_id, app_secret, email|username|mobile(+country_code), password (хранится только SHA-256), company_id (бизнес), base_url (EU по умолчанию)
// @Tags Deye
// @Security BearerAuth
// @Param body body types.VendorAccountWrite true "Аккаунт"
// @Success 201 {object} types.VendorAccount
// @Router /deye/accounts/ [post]
func (h *DeyeHandler) CreateAccount(c *fiber.Ctx) error {
	var w types.VendorAccountWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	a, err := h.deye.CreateAccount(repositories.WithVendorActor(c.UserContext(), middleware.GetUser(c).ID), w)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(a)
}

// UpdateAccount godoc
// @Summary Изменить аккаунт Deye (пустой password — не менять)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID"
// @Param body body types.VendorAccountWrite true "Поля"
// @Success 200 {object} types.VendorAccount
// @Router /deye/accounts/{id}/ [patch]
// @Router /deye/accounts/{id}/ [put]
func (h *DeyeHandler) UpdateAccount(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var w types.VendorAccountWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	a, err := h.deye.UpdateAccount(repositories.WithVendorActor(c.UserContext(), middleware.GetUser(c).ID), id, w)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(a)
}

// DeleteAccount godoc
// @Summary Удалить аккаунт Deye
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 204
// @Router /deye/accounts/{id}/ [delete]
func (h *DeyeHandler) DeleteAccount(c *fiber.Ctx) error {
	a, _, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	if err := h.deye.DeleteAccount(repositories.WithVendorActor(c.UserContext(), middleware.GetUser(c).ID), a.ID); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// TestAccount godoc
// @Summary Проверить подключение к Deye Cloud
// @Description Требуется роль admin. Укажите ID из GET /deye/accounts. Сервер обновит токен Deye, запросит сведения об организации и сохранит результат проверки.
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} deye.AccountInfoResponse
// @Failure 401 {object} DetailResponse "Требуется вход в CRM"
// @Failure 403 {object} DetailResponse "Требуется роль admin"
// @Failure 404 {object} DetailResponse "Аккаунт не найден"
// @Failure 502 {object} DetailResponse "Ошибка Deye Cloud"
// @Failure 504 {object} DetailResponse "Истекло время ожидания Deye Cloud"
// @Router /deye/accounts/{id}/test/ [post]
func (h *DeyeHandler) TestAccount(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	info, err := h.deye.TestAccount(c.UserContext(), id)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(info)
}

// Stations godoc
// @Summary Станции аккаунта (?page&size; with_devices=1 — вместе с устройствами, device_type=INVERTER)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param page query int false "Номер страницы" minimum(1) maximum(100000) default(1)
// @Param size query int false "Записей на странице (до 50 с устройствами)" minimum(1) maximum(200) default(20)
// @Param with_devices query int false "Включить устройства" Enums(1)
// @Param device_type query string false "Тип для списка с устройствами, например INVERTER"
// @Success 200 {object} map[string]interface{}
// @Router /deye/accounts/{id}/stations/ [get]
func (h *DeyeHandler) Stations(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	if c.Query("with_devices") != "" {
		page, size := pageParams(c, 50)
		res, err := cl.StationListWithDevice(c.UserContext(), deye.StationWithDeviceRequest{
			Page: page, Size: size, DeviceType: c.Query("device_type"),
		})
		if err != nil {
			return deyeFail(c, err)
		}
		return c.JSON(res)
	}
	page, size := pageParams(c, 200)
	res, err := cl.StationList(c.UserContext(), deye.PageRequest{Page: page, Size: size})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Station godoc
// @Summary Детали и текущие данные станции
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param sid path int true "ID станции Deye"
// @Success 200 {object} map[string]interface{}
// @Router /deye/accounts/{id}/stations/{sid}/ [get]
func (h *DeyeHandler) Station(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	sid, ok := paramID(c, "sid")
	if !ok {
		return notFound(c)
	}
	det, err := cl.StationDetail(c.UserContext(), sid)
	if err != nil {
		return deyeFail(c, err)
	}
	latest, err := cl.StationLatest(c.UserContext(), sid)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(fiber.Map{"station": det.Station, "latest": latest})
}

// StationDevices godoc
// @Summary Устройства станции
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param sid path int true "ID станции Deye"
// @Param page query int false "Номер страницы" minimum(1) maximum(100000) default(1)
// @Param size query int false "Записей на странице" minimum(1) maximum(200) default(20)
// @Success 200 {object} deye.StationDeviceResponse
// @Router /deye/accounts/{id}/stations/{sid}/devices/ [get]
func (h *DeyeHandler) StationDevices(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	sid, ok := paramID(c, "sid")
	if !ok {
		return notFound(c)
	}
	page, size := pageParams(c, 200)
	res, err := cl.StationDevices(c.UserContext(), deye.StationDeviceRequest{StationIDs: []int64{sid}, Page: page, Size: size})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// StationCreate godoc
// @Summary Создать станцию в Deye Cloud
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param body body deye.StationCreateRequest true "Станция"
// @Success 200 {object} deye.StationCreateResponse
// @Router /deye/accounts/{id}/stations/create/ [post]
func (h *DeyeHandler) StationCreate(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	var req deye.StationCreateRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	res, err := cl.StationCreate(c.UserContext(), req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Devices godoc
// @Summary Все устройства аккаунта (бизнес-аккаунт), ?page&size (до 200)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param page query int false "Номер страницы" minimum(1) maximum(100000) default(1)
// @Param size query int false "Записей на странице" minimum(1) maximum(200) default(20)
// @Success 200 {object} deye.DeviceListResponse
// @Router /deye/accounts/{id}/devices/ [get]
func (h *DeyeHandler) Devices(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	page, size := pageParams(c, 200)
	res, err := cl.DeviceList(c.UserContext(), deye.PageRequest{Page: page, Size: size})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// DeviceRegister godoc
// @Summary Добавить логгер в станцию {deviceSn, gatewaySn, stationId}
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param body body deye.DeviceRegisterRequest true "Логгер"
// @Success 200 {object} deye.DeviceRegisterResponse
// @Router /deye/accounts/{id}/devices/register/ [post]
func (h *DeyeHandler) DeviceRegister(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	var req deye.DeviceRegisterRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	res, err := cl.DeviceRegister(c.UserContext(), req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Loggers godoc
// @Summary Добавить (add) или убрать (delete) логгеры бизнес-аккаунта {deviceSns: [...]} — до 10 за раз
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param action path string true "Действие" Enums(add,delete)
// @Param body body deye.LoggerRequest true "SN логгеров"
// @Success 200 {object} deye.BaseResponse
// @Router /deye/accounts/{id}/loggers/{action}/ [post]
func (h *DeyeHandler) Loggers(c *fiber.Ctx) error {
	_, cl, err := h.account(c)
	if err != nil {
		return fail(c, err)
	}
	var req deye.LoggerRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	if len(req.DeviceSns) == 0 || len(req.DeviceSns) > 10 {
		return c.Status(fiber.StatusBadRequest).JSON(services.Field("deviceSns", "Укажите от 1 до 10 SN."))
	}
	var res *deye.BaseResponse
	switch c.Params("action") {
	case "add":
		res, err = cl.AddLoggers(c.UserContext(), req.DeviceSns)
	case "delete":
		res, err = cl.DeleteLoggers(c.UserContext(), req.DeviceSns)
	default:
		return notFound(c)
	}
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Import godoc
// @Summary Подключить устройство Deye как модем клиента {device_sn, station_id, client, site, name, has_generator, sections, invert_battery, invert_grid, station_power_scale}
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID аккаунта"
// @Param body body services.DeyeImportRequest true "Импорт"
// @Success 201 {object} types.ModemView
// @Router /deye/accounts/{id}/import/ [post]
func (h *DeyeHandler) Import(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var req services.DeyeImportRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	m, err := h.deye.Import(c.UserContext(), id, req)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(m)
}

// Sync godoc
// @Summary Опросить облака производителей прямо сейчас
// @Tags Deye
// @Security BearerAuth
// @Success 200 {object} map[string]bool
// @Router /deye/sync/ [post]
func (h *DeyeHandler) Sync(c *fiber.Ctx) error {
	h.collector.PollAll(c.UserContext())
	return c.JSON(fiber.Map{"ok": true})
}

// ─── модем Deye ──────────────────────────────────────────────────────────────

// target — модем (доступный пользователю) и его клиент Deye.
func (h *DeyeHandler) target(c *fiber.Ctx) (*services.DeyeTarget, error) {
	id, ok := paramID(c, "id")
	if !ok {
		return nil, services.Detail(fiber.StatusNotFound, "Страница не найдена.")
	}
	m, err := h.modems.Get(c.UserContext(), middleware.GetUser(c), id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, services.Detail(fiber.StatusNotFound, "Страница не найдена.")
	}
	return h.deye.Target(c.UserContext(), &m.Modem)
}

func (h *DeyeHandler) stationOf(t *services.DeyeTarget) (int64, error) {
	if t.StationID == nil {
		return 0, services.Detail(fiber.StatusBadRequest, "У модема не указана станция Deye (connection.station_id).")
	}
	return *t.StationID, nil
}

// unixOrDate — ?from= / ?to= как unix-секунды или дата.
func (h *DeyeHandler) unixOrDate(c *fiber.Ctx, name string, def time.Time) (int64, bool) {
	v := c.Query(name)
	if v == "" {
		return def.Unix(), true
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n > 1e12 { // миллисекунды
			n /= 1000
		}
		return n, true
	}
	t, ok := parseTime(v, h.loc)
	if !ok || t == nil {
		return 0, false
	}
	return t.Unix(), true
}

// Latest godoc
// @Summary Сырые последние показания устройства Deye
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Success 200 {object} deye.DeviceLatestResponse
// @Router /modems/{id}/deye/latest/ [get]
func (h *DeyeHandler) Latest(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	res, err := t.Client.DeviceLatest(c.UserContext(), deye.DeviceLatestRequest{DeviceList: []string{t.DeviceSn}})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// StationLatest godoc
// @Summary Текущие данные станции Deye
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Success 200 {object} deye.StationLatestResponse
// @Router /modems/{id}/deye/station/ [get]
func (h *DeyeHandler) StationLatest(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	sid, err := h.stationOf(t)
	if err != nil {
		return fail(c, err)
	}
	res, err := t.Client.StationLatest(c.UserContext(), sid)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// MeasurePoints godoc
// @Summary Доступные точки измерения устройства
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param device_type query string false "Тип устройства, например INVERTER"
// @Success 200 {object} deye.MeasurePointsResponse
// @Router /modems/{id}/deye/measure-points/ [get]
func (h *DeyeHandler) MeasurePoints(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	res, err := t.Client.DeviceMeasurePoints(c.UserContext(), deye.DeviceSnRequest{DeviceSn: t.DeviceSn, DeviceType: c.Query("device_type")})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// History godoc
// @Summary История устройства {granularity: 1..4, startAt, endAt, measurePoints}
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param body body deye.DeviceHistoryRequest true "Параметры (deviceSn подставляется сам)"
// @Success 200 {object} deye.DeviceHistoryResponse
// @Router /modems/{id}/deye/history/ [post]
func (h *DeyeHandler) History(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	var req deye.DeviceHistoryRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	req.DeviceSn = t.DeviceSn
	if req.Granularity == 0 {
		req.Granularity = 2
	}
	if req.StartAt == "" {
		now := time.Now().In(h.loc)
		req.StartAt = now.AddDate(0, 0, -7).Format("2006-01-02")
		req.EndAt = now.Format("2006-01-02")
	}
	res, err := t.Client.DeviceHistory(c.UserContext(), req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// HistoryRaw godoc
// @Summary История по точкам {startTimestamp, endTimestamp (≤5 дней), measurePoints}
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param body body deye.DeviceHistoryRawRequest true "Параметры (deviceSn подставляется сам)"
// @Success 200 {object} deye.DeviceHistoryResponse
// @Router /modems/{id}/deye/history-raw/ [post]
func (h *DeyeHandler) HistoryRaw(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	var req deye.DeviceHistoryRawRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	req.DeviceSn = t.DeviceSn
	if req.EndTimestamp == 0 {
		req.EndTimestamp = time.Now().Unix()
	}
	if req.StartTimestamp == 0 {
		req.StartTimestamp = req.EndTimestamp - 24*3600
	}
	if len(req.MeasurePoints) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(services.Field("measurePoints",
			"Укажите точки (список: GET /api/modems/{id}/deye/measure-points/)."))
	}
	res, err := t.Client.DeviceHistoryRaw(c.UserContext(), req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// StationHistory godoc
// @Summary История станции {granularity: 1 кадры/2 дни/3 месяцы/4 годы, startAt, endAt}
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param body body deye.StationHistoryRequest true "Параметры (stationId подставляется сам)"
// @Success 200 {object} deye.StationHistoryResponse
// @Router /modems/{id}/deye/station-history/ [post]
func (h *DeyeHandler) StationHistory(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	sid, err := h.stationOf(t)
	if err != nil {
		return fail(c, err)
	}
	var req deye.StationHistoryRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	req.StationID = sid
	if req.Granularity == 0 {
		req.Granularity = 1
	}
	if req.StartAt == "" {
		req.StartAt = time.Now().In(h.loc).Format("2006-01-02")
	}
	res, err := t.Client.StationHistory(c.UserContext(), req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// StationHistoryPower godoc
// @Summary История мощностей станции {startTimestamp, endTimestamp} (≤12 месяцев)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param body body deye.StationHistoryPowerRequest true "Параметры (stationId подставляется сам)"
// @Success 200 {object} deye.StationHistoryResponse
// @Router /modems/{id}/deye/station-history-power/ [post]
func (h *DeyeHandler) StationHistoryPower(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	sid, err := h.stationOf(t)
	if err != nil {
		return fail(c, err)
	}
	var req deye.StationHistoryPowerRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	req.StationID = sid
	if req.EndTimestamp == 0 {
		req.EndTimestamp = time.Now().Unix()
	}
	if req.StartTimestamp == 0 {
		req.StartTimestamp = req.EndTimestamp - 24*3600
	}
	res, err := t.Client.StationHistoryPower(c.UserContext(), req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Alerts godoc
// @Summary Аварии устройства в Deye Cloud (?from&to — unix или даты, по умолчанию 7 дней; ≤30 дней), ?page&size
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param page query int false "Номер страницы" minimum(1) maximum(100000) default(1)
// @Param size query int false "Записей на странице" minimum(1) maximum(100) default(20)
// @Param from query string false "Начало периода, дата или Unix timestamp"
// @Param to query string false "Конец периода, дата или Unix timestamp"
// @Success 200 {object} deye.DeviceAlertListResponse
// @Router /modems/{id}/deye/alerts/ [get]
func (h *DeyeHandler) Alerts(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	now := time.Now()
	to, ok1 := h.unixOrDate(c, "to", now)
	from, ok2 := h.unixOrDate(c, "from", now.AddDate(0, 0, -7))
	if !ok1 || !ok2 {
		return detail(c, fiber.StatusBadRequest, "Неверный формат from/to")
	}
	page, size := pageParams(c, 100)
	res, err := t.Client.DeviceAlertList(c.UserContext(), deye.DeviceAlertListRequest{
		DeviceSn: t.DeviceSn, StartTimestamp: from, EndTimestamp: to, Page: page, Size: size,
	})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// StationAlerts godoc
// @Summary Аварии станции в Deye Cloud (?from&to, по умолчанию 30 дней; ≤180 дней), ?page&size
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param page query int false "Номер страницы" minimum(1) maximum(100000) default(1)
// @Param size query int false "Записей на странице" minimum(1) maximum(200) default(20)
// @Param from query string false "Начало периода, дата или Unix timestamp"
// @Param to query string false "Конец периода, дата или Unix timestamp"
// @Success 200 {object} deye.StationAlertListResponse
// @Router /modems/{id}/deye/station-alerts/ [get]
func (h *DeyeHandler) StationAlerts(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	sid, err := h.stationOf(t)
	if err != nil {
		return fail(c, err)
	}
	now := time.Now()
	to, ok1 := h.unixOrDate(c, "to", now)
	from, ok2 := h.unixOrDate(c, "from", now.AddDate(0, 0, -30))
	if !ok1 || !ok2 {
		return detail(c, fiber.StatusBadRequest, "Неверный формат from/to")
	}
	page, size := pageParams(c, 200)
	res, err := t.Client.StationAlertList(c.UserContext(), deye.StationAlertListRequest{
		StationID: sid, StartTimestamp: from, EndTimestamp: to, Page: page, Size: size,
	})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Config godoc
// @Summary Настройки инвертора: батарея, режим работы, TOU (?part=battery|system|tou — только одна часть)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param part query string false "Раздел настроек" Enums(battery,system,tou)
// @Success 200 {object} map[string]interface{}
// @Router /modems/{id}/deye/config/ [get]
func (h *DeyeHandler) Config(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	ctx := c.UserContext()
	out := fiber.Map{}
	part := c.Query("part")
	if part == "" || part == "battery" {
		r, err := t.Client.ConfigBattery(ctx, t.DeviceSn)
		if err != nil {
			return deyeFail(c, err)
		}
		out["battery"] = r
	}
	if part == "" || part == "system" {
		r, err := t.Client.ConfigSystem(ctx, t.DeviceSn)
		if err != nil {
			return deyeFail(c, err)
		}
		out["system"] = r
	}
	if part == "" || part == "tou" {
		r, err := t.Client.ConfigTOU(ctx, t.DeviceSn)
		if err != nil {
			return deyeFail(c, err)
		}
		out["tou"] = r
	}
	return c.JSON(out)
}

// controlFunc — одна команда: разобрать тело, подставить deviceSn, отправить.
type controlFunc func(ctx context.Context, cl *deye.Client, sn string, body []byte) (*deye.ControlResponse, error)

func ctrl[T any](setSn func(*T, string), call func(*deye.Client, context.Context, T) (*deye.ControlResponse, error)) controlFunc {
	return func(ctx context.Context, cl *deye.Client, sn string, body []byte) (*deye.ControlResponse, error) {
		var req T
		if len(body) > 0 {
			if err := json.Unmarshal(body, &req); err != nil {
				return nil, services.Detail(fiber.StatusBadRequest, "Неверный JSON: "+err.Error())
			}
		}
		setSn(&req, sn)
		return call(cl, ctx, req)
	}
}

// DeyeCommands — команды инвертору: /api/modems/{id}/deye/control/{command}/.
var DeyeCommands = map[string]controlFunc{
	// {batteryModeType: GRID_CHARGE|GEN_CHARGE, action: on|off}
	"battery-mode": ctrl(func(r *deye.BatteryModeRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).BatteryModeControl),
	// {paramterType: MAX_CHARGE_CURRENT|MAX_DISCHARGE_CURRENT|GRID_CHARGE_AMPERE|BATT_LOW, value}
	"battery-parameter": ctrl(func(r *deye.BatteryParameterRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).BatteryParameterUpdate),
	// {batteryType: BATT_V|BATT_SOC|LI|NO_BATTERY}
	"battery-type": ctrl(func(r *deye.BatteryTypeRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).BatteryTypeUpdate),
	// {action: on|off, power}
	"peak-shaving": ctrl(func(r *deye.PeakShavingRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).GridPeakShavingControl),
	// {onSOC, offSOC | onVoltage, offVoltage, onGridAlwaysOn}
	"smartload": ctrl(func(r *deye.SmartLoadRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).SmartLoadUpdate),
	// {energyPattern: BATTERY_FIRST|LOAD_FIRST}
	"energy-pattern": ctrl(func(r *deye.EnergyPatternRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).EnergyPatternUpdate),
	// {limitControlFunctionType: SELL_FIRST|ZERO_EXPORT_TO_UPS_LOAD|ZERO_EXPORT_TO_CT|ZERO_EXPORT_TO_WIRELESS_CT}
	"limit-control": ctrl(func(r *deye.LimitControlRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).LimitControl),
	// {powerType: MAX_SELL_POWER|MAX_SOLAR_POWER|ZERO_EXPORT_POWER, value}
	"power": ctrl(func(r *deye.PowerRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).PowerUpdate),
	// {action: on|off}
	"solar-sell": ctrl(func(r *deye.ActionRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).SolarSellControl),
	// {action: on|off, days: [MONDAY, ...]}
	"tou-switch": ctrl(func(r *deye.TOUSwitchRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).TOUSwitch),
	// {timeUseSettingItems: [6 интервалов]}
	"tou": ctrl(func(r *deye.TOUSetRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).TOUUpdate),
	// {workMode: SELLING_FIRST|ZERO_EXPORT_TO_LOAD|ZERO_EXPORT_TO_CT}
	"work-mode": ctrl(func(r *deye.WorkModeRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).WorkModeUpdate),
	// несколько настроек одной командой
	"dynamic": ctrl(func(r *deye.DynamicControlRequest, sn string) { r.DeviceSn = sn }, (*deye.Client).DynamicControl),
}

// Control godoc
// @Summary Отправить команду управления инвертором
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param command path string true "Команда" Enums(battery-mode,battery-parameter,battery-type,peak-shaving,smartload,energy-pattern,limit-control,power,solar-sell,tou-switch,tou,work-mode,dynamic)
// @Param body body map[string]interface{} true "JSON выбранной команды. Сервер подставляет deviceSn."
// @Success 200 {object} deye.ControlResponse
// @Router /modems/{id}/deye/control/{command}/ [post]
func (h *DeyeHandler) Control(c *fiber.Ctx) error {
	fn, ok := DeyeCommands[c.Params("command")]
	if !ok {
		return detail(c, fiber.StatusNotFound, "Неизвестная команда.")
	}
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	res, err := fn(c.UserContext(), t.Client, t.DeviceSn, c.Body())
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// DynamicRead godoc
// @Summary Запросить чтение текущих настроек (затем GET .../dynamic/result/{orderId}/)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Success 200 {object} deye.ControlResponse
// @Router /modems/{id}/deye/dynamic/read/ [post]
func (h *DeyeHandler) DynamicRead(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	res, err := t.Client.DynamicControlRead(c.UserContext(), t.DeviceSn)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// DynamicResult godoc
// @Summary Результат чтения настроек
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param orderId path int true "orderId"
// @Success 200 {object} deye.DynamicConfigResponse
// @Router /modems/{id}/deye/dynamic/result/{orderId}/ [get]
func (h *DeyeHandler) DynamicResult(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	orderID, ok := paramID(c, "orderId")
	if !ok {
		return notFound(c)
	}
	res, err := t.Client.DynamicControlReadResult(c.UserContext(), orderID)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}

// Order godoc
// @Summary Результат команды (status: 0 ждёт, 100 отправляется, 500 ошибка, 666 выполнено)
// @Tags Deye
// @Security BearerAuth
// @Param id path int true "ID модема"
// @Param orderId path int true "orderId"
// @Success 200 {object} deye.OrderResponse
// @Router /modems/{id}/deye/orders/{orderId}/ [get]
func (h *DeyeHandler) Order(c *fiber.Ctx) error {
	t, err := h.target(c)
	if err != nil {
		return fail(c, err)
	}
	orderID, ok := paramID(c, "orderId")
	if !ok {
		return notFound(c)
	}
	res, err := t.Client.GetOrder(c.UserContext(), orderID)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(res)
}
