package api

import (
	"strings"

	"ata-energo/db/repositories"
	"ata-energo/middleware"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

// DiscoveryHandler — поиск инверторов (только admin и it_head).
//
//	/api/agents/                          агенты: список, создание (токен показывается 1 раз), удаление
//	/api/agents/{id}/new_token/           выдать новый токен
//	POST /api/discovery/scan/             {agent, method: wifi|bluetooth|all, subnet?} -> задача
//	GET  /api/discovery/tasks/{id}/       статус задачи + найденные устройства
//	GET  /api/discovery/devices/          все найденные устройства (?agent=)
//	POST /api/discovery/devices/{id}/probe/    проверить совместимость (чтение показаний)
//	POST /api/discovery/devices/{id}/connect/  подключить к мониторингу {client, site, name, ...}
//	POST /api/discovery/devices/clear/    очистить список (кроме подключённых)
//	POST /api/discovery/browser-bluetooth/ сохранить устройство, найденное Web Bluetooth в браузере
type DiscoveryHandler struct {
	discovery *services.DiscoveryService
	clients   repositories.ClientRepository
}

func NewDiscoveryHandler(discovery *services.DiscoveryService, clients repositories.ClientRepository) *DiscoveryHandler {
	return &DiscoveryHandler{discovery: discovery, clients: clients}
}

// serverURL — адрес сайта для команды запуска агента.
func serverURL(c *fiber.Ctx) string {
	return strings.TrimRight(c.BaseURL(), "/")
}

// ListAgents godoc
// @Summary Агенты поиска
// @Tags Discovery
// @Security BearerAuth
// @Success 200 {array} types.Agent
// @Router /agents/ [get]
func (h *DiscoveryHandler) ListAgents(c *fiber.Ctx) error {
	list, err := h.discovery.ListAgents(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// GetAgent godoc
// @Summary Агент
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.Agent
// @Router /agents/{id}/ [get]
func (h *DiscoveryHandler) GetAgent(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	a, err := h.discovery.GetAgent(c.UserContext(), id)
	if err != nil {
		return err
	}
	if a == nil {
		return notFound(c)
	}
	return c.JSON(a)
}

// CreateAgent godoc
// @Summary Создать агента. Ответ содержит token и command (показываются один раз)
// @Tags Discovery
// @Security BearerAuth
// @Param body body types.AgentWrite true "Агент"
// @Success 201 {object} map[string]interface{}
// @Router /agents/ [post]
func (h *DiscoveryHandler) CreateAgent(c *fiber.Ctx) error {
	var w types.AgentWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	a, token, err := h.discovery.CreateAgent(c.UserContext(), w)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(struct {
		*types.Agent
		Token   string `json:"token"`
		Command string `json:"command"`
	}{a, token, services.AgentCommand(serverURL(c), token)})
}

// UpdateAgent godoc
// @Summary Изменить агента (name, client, is_active)
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID"
// @Param body body types.AgentWrite true "Поля"
// @Success 200 {object} types.Agent
// @Router /agents/{id}/ [patch]
// @Router /agents/{id}/ [put]
func (h *DiscoveryHandler) UpdateAgent(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var w types.AgentWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	a, err := h.discovery.UpdateAgent(c.UserContext(), id, w)
	if err != nil {
		return fail(c, err)
	}
	if a == nil {
		return notFound(c)
	}
	return c.JSON(a)
}

// DeleteAgent godoc
// @Summary Удалить агента
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 204
// @Router /agents/{id}/ [delete]
func (h *DiscoveryHandler) DeleteAgent(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	a, err := h.discovery.GetAgent(c.UserContext(), id)
	if err != nil {
		return err
	}
	if a == nil {
		return notFound(c)
	}
	if err := h.discovery.DeleteAgent(c.UserContext(), id); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// NewToken godoc
// @Summary Выдать агенту новый токен (старый перестаёт работать)
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} map[string]string
// @Router /agents/{id}/new_token/ [post]
func (h *DiscoveryHandler) NewToken(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	a, err := h.discovery.GetAgent(c.UserContext(), id)
	if err != nil {
		return err
	}
	if a == nil {
		return notFound(c)
	}
	token, err := h.discovery.NewToken(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"token": token, "command": services.AgentCommand(serverURL(c), token)})
}

type scanRequest struct {
	Agent  int64  `json:"agent"`
	Method string `json:"method" example:"all"`
	Subnet string `json:"subnet" example:"192.168.1.0/24"`
}

// StartScan godoc
// @Summary Начать поиск: method = wifi | bluetooth | all
// @Tags Discovery
// @Security BearerAuth
// @Param body body scanRequest true "Задача"
// @Success 201 {object} types.AgentTask
// @Router /discovery/scan/ [post]
func (h *DiscoveryHandler) StartScan(c *fiber.Ctx) error {
	var req scanRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	t, err := h.discovery.StartScan(c.UserContext(), req.Agent, req.Method, req.Subnet, middleware.GetUser(c).ID)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(t)
}

// TaskDetail godoc
// @Summary Ход поиска и найденные устройства
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID задачи"
// @Success 200 {object} types.AgentTask
// @Router /discovery/tasks/{id}/ [get]
func (h *DiscoveryHandler) TaskDetail(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	t, err := h.discovery.TaskDetail(c.UserContext(), id)
	if err != nil {
		return err
	}
	if t == nil {
		return notFound(c)
	}
	return c.JSON(t)
}

// DeviceList godoc
// @Summary Все найденные устройства (новые сверху, до 500). ?agent=ID
// @Tags Discovery
// @Security BearerAuth
// @Param agent query int false "ID агента"
// @Success 200 {array} types.FoundDevice
// @Router /discovery/devices/ [get]
func (h *DiscoveryHandler) DeviceList(c *fiber.Ctx) error {
	list, err := h.discovery.ListDevices(c.UserContext(), queryInt64(c, "agent"))
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// DeviceProbe godoc
// @Summary Проверить, можно ли прочитать показания (задача агенту). Можно передать protocol, port, unit_id, logger_serial, profile, invert_battery, invert_grid
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID устройства"
// @Param body body map[string]interface{} false "Параметры протокола"
// @Success 201 {object} types.AgentTask
// @Router /discovery/devices/{id}/probe/ [post]
func (h *DiscoveryHandler) DeviceProbe(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	d, err := h.discovery.GetFound(c.UserContext(), id)
	if err != nil {
		return err
	}
	if d == nil {
		return notFound(c)
	}
	data, err := bodyMap(c)
	if err != nil {
		return detail(c, fiber.StatusBadRequest, "Неверный JSON")
	}
	t, err := h.discovery.Probe(c.UserContext(), d, data, middleware.GetUser(c).ID)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(t)
}

// DeviceConnect godoc
// @Summary Подключить проверенное устройство к объекту клиента {client, site, name, has_generator, poll_interval, invert_battery, invert_grid}
// @Tags Discovery
// @Security BearerAuth
// @Param id path int true "ID устройства"
// @Param body body map[string]interface{} true "Клиент и параметры подключения"
// @Success 200 {object} map[string]interface{}
// @Router /discovery/devices/{id}/connect/ [post]
func (h *DiscoveryHandler) DeviceConnect(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	d, err := h.discovery.GetFound(c.UserContext(), id)
	if err != nil {
		return err
	}
	if d == nil {
		return notFound(c)
	}
	data, err := bodyMap(c)
	if err != nil {
		return detail(c, fiber.StatusBadRequest, "Неверный JSON")
	}
	if ok, _ := d.CheckResult["ok"].(bool); !ok {
		return detail(c, fiber.StatusBadRequest, "Устройство обнаружено, требуется настройка протокола. "+
			"Сначала проверка должна прочитать реальные показания.")
	}
	clientID := services.ToNumber(data["client"])
	if clientID == nil {
		return notFound(c)
	}
	client, err := h.clients.GetByID(c.UserContext(), int64(*clientID))
	if err != nil {
		return err
	}
	if client == nil {
		return notFound(c)
	}
	opts := map[string]any{}
	for _, key := range []string{"invert_battery", "invert_grid"} {
		if _, has := data[key]; has {
			opts[key] = pickBool(data, key)
		}
	}
	poll := 10
	if p := services.ToNumber(data["poll_interval"]); p != nil {
		poll = int(*p)
	}
	str := func(k string) string { return strings.TrimSpace(services.AsString(data[k])) }
	m, err := h.discovery.Connect(c.UserContext(), d, client, services.ConnectOptions{
		Site: str("site"), Name: str("name"), HasGenerator: pickBool(data, "has_generator"),
		PollInterval: poll, Options: opts,
	})
	if err != nil {
		return fail(c, err)
	}
	fresh, err := h.discovery.GetFound(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"modem_id": m.ID, "serial": m.Serial, "client": client.ID, "device": fresh})
}

// DevicesClear godoc
// @Summary Удалить из списка найденные устройства, которые не подключены
// @Tags Discovery
// @Security BearerAuth
// @Success 200 {object} map[string]int
// @Router /discovery/devices/clear/ [post]
func (h *DiscoveryHandler) DevicesClear(c *fiber.Ctx) error {
	n, err := h.discovery.ClearDevices(c.UserContext())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"deleted": n})
}

// BrowserBluetooth godoc
// @Summary Сохранить устройство, найденное в браузере через Web Bluetooth {id, name, services, connected, error}
// @Tags Discovery
// @Security BearerAuth
// @Param body body map[string]interface{} true "Данные обнаруженного устройства"
// @Success 201 {object} types.FoundDevice
// @Router /discovery/browser-bluetooth/ [post]
func (h *DiscoveryHandler) BrowserBluetooth(c *fiber.Ctx) error {
	data, err := bodyMap(c)
	if err != nil {
		return detail(c, fiber.StatusBadRequest, "Неверный JSON")
	}
	d, err := h.discovery.BrowserBluetooth(c.UserContext(), data)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(d)
}
