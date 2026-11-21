package api

import (
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

// ClientHandler — /api/clients/ (admin, it_head — CRUD; dispatcher — просмотр).
type ClientHandler struct {
	clients *services.ClientService
}

func NewClientHandler(clients *services.ClientService) *ClientHandler {
	return &ClientHandler{clients: clients}
}

// List godoc
// @Summary Клиенты (+ модемы, количество устройств). Поиск: ?search=
// @Tags Clients
// @Security BearerAuth
// @Produce json
// @Success 200 {array} types.ClientView
// @Router /clients/ [get]
func (h *ClientHandler) List(c *fiber.Ctx) error {
	list, err := h.clients.List(c.UserContext(), c.Query("search"))
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// Get godoc
// @Summary Клиент
// @Tags Clients
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.ClientView
// @Router /clients/{id}/ [get]
func (h *ClientHandler) Get(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	v, err := h.clients.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	if v == nil {
		return notFound(c)
	}
	return c.JSON(v)
}

// Create godoc
// @Summary Создать клиента (+ логин для входа, + модем — устройства создаются сами)
// @Tags Clients
// @Security BearerAuth
// @Accept json
// @Param body body types.ClientWrite true "Клиент"
// @Success 201 {object} types.ClientView
// @Router /clients/ [post]
func (h *ClientHandler) Create(c *fiber.Ctx) error {
	var w types.ClientWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	v, err := h.clients.Create(c.UserContext(), w)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(v)
}

// Update godoc
// @Summary Изменить клиента, его логин и основной модем
// @Tags Clients
// @Security BearerAuth
// @Accept json
// @Param id path int true "ID"
// @Param body body types.ClientWrite true "Поля"
// @Success 200 {object} types.ClientView
// @Router /clients/{id}/ [patch]
func (h *ClientHandler) Update(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var w types.ClientWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	v, err := h.clients.Update(c.UserContext(), id, w)
	if err != nil {
		return fail(c, err)
	}
	if v == nil {
		return notFound(c)
	}
	return c.JSON(v)
}

// Delete godoc
// @Summary Удалить клиента вместе с его аккаунтом
// @Tags Clients
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 204
// @Router /clients/{id}/ [delete]
func (h *ClientHandler) Delete(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	v, err := h.clients.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	if v == nil {
		return notFound(c)
	}
	if err := h.clients.Delete(c.UserContext(), id); err != nil {
		return fail(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// Devices godoc
// @Summary Всё оборудование клиента
// @Tags Clients
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {array} types.DeviceView
// @Router /clients/{id}/devices/ [get]
func (h *ClientHandler) Devices(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	v, err := h.clients.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	if v == nil {
		return notFound(c)
	}
	list, err := h.clients.Devices(c.UserContext(), id)
	if err != nil {
		return err
	}
	return c.JSON(list)
}
