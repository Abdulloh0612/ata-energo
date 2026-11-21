package middleware

import (
	"strings"

	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
)

const userKey = "user"

func detail(c *fiber.Ctx, status int, msg string) error {
	return c.Status(status).JSON(fiber.Map{"detail": msg})
}

// JWTAuth — заголовок "Authorization: Bearer <access>". Пользователь берётся из базы
// (проверяется is_active и актуальная роль) и кладётся в c.Locals("user").
func JWTAuth(auth *services.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		if header == "" {
			return detail(c, fiber.StatusUnauthorized, "Учетные данные не были предоставлены.")
		}
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return detail(c, fiber.StatusUnauthorized, "Неверный заголовок Authorization.")
		}
		u, err := auth.UserFromAccess(c.UserContext(), strings.TrimSpace(parts[1]))
		if err != nil {
			return err
		}
		if u == nil {
			return detail(c, fiber.StatusUnauthorized, "Токен недействителен или просрочен")
		}
		c.Locals(userKey, u)
		return c.Next()
	}
}

// GetUser — текущий пользователь (после JWTAuth).
func GetUser(c *fiber.Ctx) *types.User {
	u, _ := c.Locals(userKey).(*types.User)
	return u
}

func isSafe(method string) bool {
	return method == fiber.MethodGet || method == fiber.MethodHead || method == fiber.MethodOptions
}

// ManagerOnly — доступ только для admin и it_head (полные права).
func ManagerOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !GetUser(c).IsManager() {
			return detail(c, fiber.StatusForbidden, "Нужна роль admin или it_head.")
		}
		return c.Next()
	}
}

// StaffOnly — доступ для admin, it_head и dispatcher (видят всех клиентов).
func StaffOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !GetUser(c).IsStaffViewer() {
			return detail(c, fiber.StatusForbidden, "Нужна роль admin, it_head или dispatcher.")
		}
		return c.Next()
	}
}

// ClientOnly — доступ только для клиента (role = user).
func ClientOnly() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if u := GetUser(c); u == nil || u.Role != types.RoleUser {
			return detail(c, fiber.StatusForbidden, "Этот адрес только для клиентов.")
		}
		return c.Next()
	}
}

// StaffReadManagerWrite — чтение (GET) для admin/it_head/dispatcher, изменение — только admin/it_head.
func StaffReadManagerWrite() fiber.Handler {
	return func(c *fiber.Ctx) error {
		u := GetUser(c)
		if isSafe(c.Method()) {
			if !u.IsStaffViewer() {
				return detail(c, fiber.StatusForbidden, "Нужна роль admin, it_head или dispatcher.")
			}
		} else if !u.IsManager() {
			return detail(c, fiber.StatusForbidden, "Изменять данные могут только admin и it_head.")
		}
		return c.Next()
	}
}

// ReadAnyManagerWrite — чтение всем вошедшим (список потом фильтруется по роли), изменение — admin/it_head.
func ReadAnyManagerWrite() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !isSafe(c.Method()) && !GetUser(c).IsManager() {
			return detail(c, fiber.StatusForbidden, "Изменять данные могут только admin и it_head.")
		}
		return c.Next()
	}
}

const agentKey = "agent"

// AgentAuth — эндпоинты локального агента: заголовок X-Agent-Token (без JWT).
func AgentAuth(discovery *services.DiscoveryService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ip := strings.TrimSpace(strings.Split(c.Get("X-Forwarded-For"), ",")[0])
		if ip == "" {
			ip = c.IP()
		}
		a, err := discovery.AgentByToken(c.UserContext(), c.Get("X-Agent-Token"), ip)
		if err != nil {
			return err
		}
		if a == nil {
			return detail(c, fiber.StatusUnauthorized, "Неверный токен агента")
		}
		c.Locals(agentKey, a)
		return c.Next()
	}
}

// GetAgent — текущий агент (после AgentAuth).
func GetAgent(c *fiber.Ctx) *types.Agent {
	a, _ := c.Locals(agentKey).(*types.Agent)
	return a
}
