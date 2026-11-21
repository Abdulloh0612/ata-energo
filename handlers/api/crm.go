package api

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"ata-energo/middleware"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type workAccess struct {
	View   bool `json:"view"`
	Create bool `json:"create"`
	Edit   bool `json:"edit"`
	Delete bool `json:"delete"`
	Assign bool `json:"assign"`
	Own    bool `json:"own"`
}

func crmAccess(role, kind string) workAccess {
	switch role {
	case types.RoleAdmin, types.RoleITHead:
		return workAccess{View: true, Create: true, Edit: true, Delete: true, Assign: true}
	case types.RoleCRMManager:
		return workAccess{View: true, Create: true, Edit: true, Own: kind == "tasks"}
	case types.RoleTechnician:
		return workAccess{View: true, Edit: true, Own: true}
	case types.RoleObserver, types.RoleDispatcher:
		return workAccess{View: kind == "requests"}
	default:
		return workAccess{}
	}
}

type workInput struct {
	Title       string     `json:"title"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	Priority    string     `json:"priority"`
	ClientID    *int64     `json:"client_id"`
	DeviceID    *int64     `json:"device_id"`
	AssigneeID  *int64     `json:"assignee_id"`
	DueAt       *time.Time `json:"due_at"`
}

func RegisterCRM(router fiber.Router, pool *pgxpool.Pool) {
	registerRoles(router, pool)
	registerCRMEquipment(router, pool)
	for _, kind := range []string{"requests", "tasks"} {
		registerWork(router.Group("/"+kind), pool, kind)
	}
	router.Get("/activity", func(c *fiber.Ctx) error {
		if middleware.GetUser(c).Role != types.RoleAdmin {
			return fiber.ErrForbidden
		}
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id DESC),'[]'::jsonb)
			FROM (SELECT * FROM crm_activity ORDER BY id DESC LIMIT 200) a`)
	})
}

func crmJSON(c *fiber.Ctx, pool *pgxpool.Pool, query string, args ...any) error {
	var body []byte
	if err := pool.QueryRow(c.UserContext(), query, args...).Scan(&body); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.ErrNotFound
		}
		return err
	}
	return c.Type("json").Send(body)
}

const workScope = ` AND (NOT $3 OR assignee_id=$2 OR ($4 AND created_by=$2))`

const workAccessKey = "crm_work_access"

func registerWork(router fiber.Router, pool *pgxpool.Pool, kind string) {
	router.Use(func(c *fiber.Ctx) error {
		grants, err := roleGrants(c.UserContext(), pool, middleware.GetUser(c).Role)
		if err != nil {
			return err
		}
		p := grants[kind]
		if !p.View {
			return fiber.ErrForbidden
		}
		c.Locals(workAccessKey, p)
		return c.Next()
	})
	router.Get("/permissions", func(c *fiber.Ctx) error {
		p := c.Locals(workAccessKey).(workAccess)
		return c.JSON(fiber.Map{"create": p.Create, "edit": p.Edit, "delete": p.Delete, "assign": p.Assign, "relations": middleware.GetUser(c).Role != types.RoleTechnician})
	})
	router.Get("/", func(c *fiber.Ctx) error {
		u := middleware.GetUser(c)
		p := c.Locals(workAccessKey).(workAccess)
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY id DESC),'[]'::jsonb)
			FROM crm_work w WHERE kind=$1`+workScope, kind, u.ID, p.Own, u.Role == types.RoleCRMManager)
	})
	router.Get("/:id", func(c *fiber.Ctx) error {
		u := middleware.GetUser(c)
		p := c.Locals(workAccessKey).(workAccess)
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return fiber.ErrNotFound
		}
		return crmJSON(c, pool, `SELECT to_jsonb(w) || jsonb_build_object('history',
			(SELECT coalesce(jsonb_agg(jsonb_build_object('id',a.id,'actor_id',a.actor_id,'action',a.action,'created_at',a.created_at)
			ORDER BY a.id DESC),'[]'::jsonb) FROM crm_activity a WHERE a.work_id=w.id))
			FROM crm_work w WHERE kind=$1`+workScope+` AND id=$5`, kind, u.ID, p.Own, u.Role == types.RoleCRMManager, id)
	})
	for _, method := range []string{fiber.MethodPost, fiber.MethodPut, fiber.MethodDelete} {
		path := "/:id"
		if method == fiber.MethodPost {
			path = "/"
		}
		router.Add(method, path, func(c *fiber.Ctx) error { return writeWork(c, pool, kind, method) })
	}
}

func writeWork(c *fiber.Ctx, pool *pgxpool.Pool, kind, method string) error {
	u := middleware.GetUser(c)
	p := c.Locals(workAccessKey).(workAccess)
	create, remove := method == fiber.MethodPost, method == fiber.MethodDelete
	if (create && !p.Create) || (remove && !p.Delete) || (!create && !remove && !p.Edit) {
		return fiber.ErrForbidden
	}

	ctx := c.UserContext()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id int64
	var old workInput
	if !create {
		id, err = strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return fiber.ErrNotFound
		}
		err = tx.QueryRow(ctx, `SELECT client_id,device_id,assignee_id FROM crm_work WHERE kind=$1`+workScope+` AND id=$5 FOR UPDATE`,
			kind, u.ID, p.Own, u.Role == types.RoleCRMManager, id).Scan(&old.ClientID, &old.DeviceID, &old.AssigneeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
	}
	var snapshot []byte
	action := "update"
	if remove {
		action = "delete"
		err = tx.QueryRow(ctx, `DELETE FROM crm_work w WHERE id=$1 RETURNING to_jsonb(w)`, id).Scan(&snapshot)
	} else {
		var in workInput
		if err := c.BodyParser(&in); err != nil {
			return fiber.NewError(400, "Проверьте поля формы и дату")
		}
		in.Title = strings.TrimSpace(in.Title)
		if in.Title == "" || len([]rune(in.Title)) > 160 || len(in.Description) > 20000 {
			return fiber.NewError(400, "Название обязательно, до 160 символов")
		}
		if in.Status != "open" && in.Status != "progress" && in.Status != "closed" {
			return fiber.NewError(400, "Неизвестный статус")
		}
		if in.Priority != "low" && in.Priority != "normal" && in.Priority != "high" {
			return fiber.NewError(400, "Неизвестный приоритет")
		}
		if !p.Assign {
			in.AssigneeID = old.AssigneeID
			if create && p.Own {
				in.AssigneeID = &u.ID
			}
		}
		if u.Role == types.RoleTechnician {
			in.ClientID, in.DeviceID = old.ClientID, old.DeviceID
		}
		var valid bool
		err = tx.QueryRow(ctx, `SELECT ($1::bigint IS NULL OR EXISTS (SELECT 1 FROM clients WHERE id=$1))
			AND ($2::bigint IS NULL OR EXISTS (SELECT 1 FROM devices WHERE id=$2 AND client_id=$1))
			AND ($3::bigint IS NULL OR EXISTS (SELECT 1 FROM users WHERE id=$3 AND role<>'user'))`,
			in.ClientID, in.DeviceID, in.AssigneeID).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return fiber.NewError(400, "Проверьте клиента, его оборудование и исполнителя")
		}
		if create {
			action = "create"
			err = tx.QueryRow(ctx, `INSERT INTO crm_work AS w (title,description,status,priority,client_id,device_id,assignee_id,due_at,kind,created_by)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id,to_jsonb(w)`,
				in.Title, in.Description, in.Status, in.Priority, in.ClientID, in.DeviceID, in.AssigneeID, in.DueAt, kind, u.ID).Scan(&id, &snapshot)
		} else {
			err = tx.QueryRow(ctx, `UPDATE crm_work w SET title=$1,description=$2,status=$3,priority=$4,client_id=$5,device_id=$6,assignee_id=$7,due_at=$8,updated_at=now()
				WHERE id=$9 RETURNING to_jsonb(w)`, in.Title, in.Description, in.Status, in.Priority, in.ClientID, in.DeviceID, in.AssigneeID, in.DueAt, id).Scan(&snapshot)
		}
	}
	if err != nil {
		return err
	}
	var auditID *int64
	if !remove {
		auditID = &id
	}
	if _, err = tx.Exec(ctx, `INSERT INTO crm_activity (work_id,actor_id,action,snapshot) VALUES ($1,$2,$3,$4::jsonb)`, auditID, u.ID, action, snapshot); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if create {
		c.Status(fiber.StatusCreated)
	}
	return c.Type("json").Send(snapshot)
}
