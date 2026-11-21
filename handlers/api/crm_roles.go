package api

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"ata-energo/middleware"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type crmRole struct {
	Code   string                `json:"code"`
	Name   string                `json:"name"`
	System bool                  `json:"system"`
	Grants map[string]workAccess `json:"grants"`
}

func defaultGrants(role string) map[string]workAccess {
	return map[string]workAccess{"requests": crmAccess(role, "requests"), "tasks": crmAccess(role, "tasks")}
}

func roleGrants(ctx context.Context, pool *pgxpool.Pool, role string) (map[string]workAccess, error) {
	var data []byte
	err := pool.QueryRow(ctx, `SELECT grants FROM crm_roles WHERE code=$1`, role).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return map[string]workAccess{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return defaultGrants(role), nil
	}
	var grants map[string]workAccess
	if err = json.Unmarshal(data, &grants); err != nil {
		return nil, err
	}
	return grants, nil
}

var customRoleCode = regexp.MustCompile(`^crm_[a-z][a-z0-9_]{1,15}$`)

func registerRoles(router fiber.Router, pool *pgxpool.Pool) {
	// @Summary Права текущего пользователя
	// @Tags CRM · Доступ
	// @Security BearerAuth
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/access [get]
	router.Get("/access", func(c *fiber.Ctx) error {
		grants, err := roleGrants(c.UserContext(), pool, middleware.GetUser(c).Role)
		if err != nil {
			return err
		}
		u := middleware.GetUser(c)
		return c.JSON(fiber.Map{"requests": grants["requests"], "tasks": grants["tasks"],
			"sections": fiber.Map{"overview": u.IsStaffViewer(), "clients": u.IsStaffViewer(), "sites": u.IsStaffViewer(),
				"devices": u.IsStaffViewer(), "alerts": u.IsStaffViewer(), "requests": grants["requests"].View,
				"tasks": grants["tasks"].View, "integrations": u.IsManager(), "admin": u.IsManager()}})
	})
	roles := router.Group("/roles", middleware.AdminOnly())
	// @Summary Роли сотрудников
	// @Tags CRM · Доступ
	// @Security BearerAuth
	// @Success 200 {array} crmRole
	// @Router /crm/roles [get]
	roles.Get("/", func(c *fiber.Ctx) error {
		rows, err := pool.Query(c.UserContext(), `SELECT code,name,system,grants FROM crm_roles WHERE code<>'user' ORDER BY system DESC,name`)
		if err != nil {
			return err
		}
		defer rows.Close()
		list := make([]crmRole, 0)
		for rows.Next() {
			var r crmRole
			var data []byte
			if err := rows.Scan(&r.Code, &r.Name, &r.System, &data); err != nil {
				return err
			}
			if len(data) == 0 {
				r.Grants = defaultGrants(r.Code)
			} else if err := json.Unmarshal(data, &r.Grants); err != nil {
				return err
			}
			list = append(list, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		return c.JSON(list)
	})
	// @Summary Создать роль
	// @Tags CRM · Доступ
	// @Security BearerAuth
	// @Param body body crmRole true "Поля запроса"
	// @Success 201 {object} crmRole
	// @Router /crm/roles [post]
	roles.Post("/", func(c *fiber.Ctx) error { return saveCRMRole(c, pool, true) })
	// @Summary Изменить роль
	// @Tags CRM · Доступ
	// @Security BearerAuth
	// @Param code path string true "Код"
	// @Param body body crmRole true "Поля запроса"
	// @Success 200 {object} crmRole
	// @Router /crm/roles/{code} [put]
	roles.Put("/:code", func(c *fiber.Ctx) error { return saveCRMRole(c, pool, false) })
	// @Summary Удалить роль
	// @Tags CRM · Доступ
	// @Security BearerAuth
	// @Param code path string true "Код"
	// @Success 204
	// @Router /crm/roles/{code} [delete]
	roles.Delete("/:code", func(c *fiber.Ctx) error {
		tag, err := pool.Exec(c.UserContext(), `DELETE FROM crm_roles WHERE code=$1 AND NOT system`, c.Params("code"))
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return fiber.NewError(409, "Переназначьте сотрудников этой роли")
		}
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fiber.ErrNotFound
		}
		return c.SendStatus(fiber.StatusNoContent)
	})
}

func saveCRMRole(c *fiber.Ctx, pool *pgxpool.Pool, create bool) error {
	var role crmRole
	if err := c.BodyParser(&role); err != nil {
		return fiber.ErrBadRequest
	}
	role.Name = strings.TrimSpace(role.Name)
	if role.Name == "" || len([]rune(role.Name)) > 80 {
		return fiber.NewError(400, "Укажите название роли, до 80 символов")
	}
	if len(role.Grants) != 2 {
		return fiber.ErrBadRequest
	}
	for _, kind := range []string{"requests", "tasks"} {
		p, ok := role.Grants[kind]
		if !ok || (!p.View && (p.Create || p.Edit || p.Delete || p.Assign || p.Own)) || (p.Assign && !p.Edit && !p.Create) {
			return fiber.NewError(400, "Действия требуют разрешения на просмотр, назначение требует права на создание или редактирование")
		}
	}
	data, err := json.Marshal(role.Grants)
	if err != nil {
		return err
	}
	if create {
		if !customRoleCode.MatchString(role.Code) {
			return fiber.NewError(400, "Код должен начинаться с crm_ и содержать строчные латинские буквы, цифры или подчёркивание")
		}
		if _, err = pool.Exec(c.UserContext(), `INSERT INTO crm_roles(code,name,system,grants) VALUES($1,$2,false,$3::jsonb)`, role.Code, role.Name, data); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return fiber.NewError(409, "Этот код роли уже используется")
			}
			return err
		}
		return c.Status(201).JSON(role)
	}
	role.Code = c.Params("code")
	if role.Code == types.RoleAdmin || role.Code == types.RoleUser {
		return fiber.ErrForbidden
	}
	tag, err := pool.Exec(c.UserContext(), `UPDATE crm_roles SET name=$2,grants=$3::jsonb WHERE code=$1`, role.Code, role.Name, data)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fiber.ErrNotFound
	}
	return c.JSON(role)
}
