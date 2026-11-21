package api

import (
	"strings"

	"ata-energo/middleware"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

type AuthHandler struct {
	auth  *services.AuthService
	users *services.UserService
}

func NewAuthHandler(auth *services.AuthService, users *services.UserService) *AuthHandler {
	return &AuthHandler{auth: auth, users: users}
}

type loginRequest struct {
	Username string `json:"username" example:"admin"`
	Password string `json:"password" example:"admin123"`
}

// Login godoc
// @Summary Вход: {username, password} -> {access, refresh, user}
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body loginRequest true "Логин и пароль"
// @Success 200 {object} services.LoginResult
// @Failure 401 {object} DetailResponse
// @Router /auth/login/ [post]
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req loginRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	errs := services.FieldErrors{}
	if strings.TrimSpace(req.Username) == "" {
		errs["username"] = []string{"Обязательное поле."}
	}
	if req.Password == "" {
		errs["password"] = []string{"Обязательное поле."}
	}
	if len(errs) > 0 {
		return c.Status(fiber.StatusBadRequest).JSON(errs)
	}
	res, err := h.auth.Login(c.UserContext(), req.Username, req.Password)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(res)
}

type refreshRequest struct {
	Refresh string `json:"refresh"`
}

// Refresh godoc
// @Summary Новый access-токен по refresh-токену
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body refreshRequest true "refresh-токен"
// @Success 200 {object} map[string]string
// @Failure 401 {object} DetailResponse
// @Router /auth/refresh/ [post]
func (h *AuthHandler) Refresh(c *fiber.Ctx) error {
	var req refreshRequest
	if err := parseBody(c, &req); err != nil {
		return err
	}
	if req.Refresh == "" {
		return c.Status(fiber.StatusBadRequest).JSON(services.Field("refresh", "Обязательное поле."))
	}
	access, err := h.auth.Refresh(c.UserContext(), req.Refresh)
	if err != nil {
		return fail(c, err)
	}
	return c.JSON(fiber.Map{"access": access})
}

// Me godoc
// @Summary Данные и роль текущего пользователя
// @Tags Auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} types.Me
// @Router /me/ [get]
func (h *AuthHandler) Me(c *fiber.Ctx) error {
	me, err := h.auth.Me(c.UserContext(), middleware.GetUser(c))
	if err != nil {
		return err
	}
	return c.JSON(me)
}

// ─── /api/users/ (admin, it_head) ────────────────────────────────────────────

// ListUsers godoc
// @Summary Пользователи (?role=, ?staff=1, ?search=)
// @Tags Users
// @Security BearerAuth
// @Produce json
// @Success 200 {array} types.UserView
// @Router /users/ [get]
func (h *AuthHandler) ListUsers(c *fiber.Ctx) error {
	list, err := h.users.List(c.UserContext(), types.UserFilter{
		Role: c.Query("role"), Staff: c.Query("staff") != "", Search: c.Query("search"),
	})
	if err != nil {
		return err
	}
	return c.JSON(list)
}

// GetUser godoc
// @Summary Пользователь
// @Tags Users
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 200 {object} types.UserView
// @Router /users/{id}/ [get]
func (h *AuthHandler) GetUser(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	u, err := h.users.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	if u == nil {
		return notFound(c)
	}
	return c.JSON(u)
}

// CreateUser godoc
// @Summary Создать пользователя (пароль минимум 6 символов)
// @Tags Users
// @Security BearerAuth
// @Accept json
// @Param body body types.UserWrite true "Пользователь"
// @Success 201 {object} types.UserView
// @Router /users/ [post]
func (h *AuthHandler) CreateUser(c *fiber.Ctx) error {
	var w types.UserWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	u, err := h.users.Create(c.UserContext(), w)
	if err != nil {
		return fail(c, err)
	}
	return c.Status(fiber.StatusCreated).JSON(u)
}

// UpdateUser godoc
// @Summary Изменить пользователя (PATCH/PUT). Если передан password — меняет пароль
// @Tags Users
// @Security BearerAuth
// @Accept json
// @Param id path int true "ID"
// @Param body body types.UserWrite true "Поля"
// @Success 200 {object} types.UserView
// @Router /users/{id}/ [patch]
func (h *AuthHandler) UpdateUser(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	var w types.UserWrite
	if err := parseBody(c, &w); err != nil {
		return err
	}
	u, err := h.users.Update(c.UserContext(), id, w)
	if err != nil {
		return fail(c, err)
	}
	if u == nil {
		return notFound(c)
	}
	return c.JSON(u)
}

// DeleteUser godoc
// @Summary Удалить пользователя (себя удалить нельзя)
// @Tags Users
// @Security BearerAuth
// @Param id path int true "ID"
// @Success 204
// @Router /users/{id}/ [delete]
func (h *AuthHandler) DeleteUser(c *fiber.Ctx) error {
	id, ok := paramID(c, "id")
	if !ok {
		return notFound(c)
	}
	u, err := h.users.Get(c.UserContext(), id)
	if err != nil {
		return err
	}
	if u == nil {
		return notFound(c)
	}
	if err := h.users.Delete(c.UserContext(), id, middleware.GetUser(c).ID); err != nil {
		return fail(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}
