package api

import (
	"strconv"
	"strings"

	"ata-energo/middleware"
	"ata-energo/services"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

type customerRequestInput struct {
	Title       string `json:"title" maxlength:"160"`
	Description string `json:"description"`
}

// Each point sums the latest panel power measurements within its interval.
// Empty intervals remain NULL so gaps cannot be mistaken for zero production.
func registerDashboard(router fiber.Router, pool *pgxpool.Pool) {
	// @Summary Выработка за период
	// @Tags CRM
	// @Security BearerAuth
	// @Param period query string false "Период" Enums(day,week,month) default(day)
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/generation [get]
	router.Get("/generation", func(c *fiber.Ctx) error {
		u := middleware.GetUser(c)
		var clientID *int64
		if !u.IsStaffViewer() {
			if u.ClientID == nil {
				return c.JSON([]any{})
			}
			clientID = u.ClientID
		}
		period := c.Query("period", "day")
		interval, bucket := "24 hours", "15 minutes"
		switch period {
		case "day":
		case "week":
			interval, bucket = "7 days", "2 hours"
		case "month":
			interval, bucket = "30 days", "6 hours"
		default:
			return fiber.ErrBadRequest
		}
		return crmJSON(c, pool, `WITH bounds AS (
		   SELECT date_bin($2::interval,now()-$1::interval,'2000-01-01'::timestamptz) AS start,
		          date_bin($2::interval,now(),'2000-01-01'::timestamptz) AS finish
		), points AS (
		   SELECT tick AS timestamp, (
		       SELECT sum(sample.power) FROM devices d
		       CROSS JOIN LATERAL (
		           SELECT t.power FROM telemetry t
		           WHERE t.device_id=d.id AND t.timestamp>=tick
		             AND t.timestamp<tick+$2::interval AND t.timestamp<=now()
		           ORDER BY t.timestamp DESC LIMIT 1
		       ) sample
		       WHERE d.device_type='panel' AND ($3::bigint IS NULL OR d.client_id=$3)
		   ) AS power FROM bounds b CROSS JOIN LATERAL generate_series(b.start,b.finish,$2::interval) tick
		) SELECT jsonb_agg(to_jsonb(p) ORDER BY timestamp) FROM points p`, interval, bucket, clientID)
	})
	// Match the latest event to the device's current condition. Reading an
	// event acknowledges the message and does not resolve the underlying fault.
	// @Summary Активные аварии оборудования
	// @Tags CRM
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/active-alerts [get]
	router.Get("/active-alerts", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(q) ORDER BY created_at DESC),'[]'::jsonb) FROM (
		  SELECT a.id,d.id AS device,d.device_id AS device_code,d.device_type,d.client_id,c.name AS client_name,
		         a.kind,a.text,a.created_at,a.is_read
		  FROM devices d JOIN clients c ON c.id=d.client_id
		  CROSS JOIN LATERAL (SELECT id,kind,text,created_at,is_read FROM alerts
		    WHERE device_id=d.id AND kind=CASE WHEN d.status='offline' THEN 'offline' ELSE 'error' END
		    ORDER BY created_at DESC,id DESC LIMIT 1) a
		  WHERE d.status='offline' OR nullif(d.last_data->>'error','') IS NOT NULL
		    OR (d.device_type='grid' AND jsonb_typeof(d.last_data->'voltage')='number' AND (d.last_data->>'voltage')::double precision<$1)
		) q`, services.GridMinVoltage)
	})
	// @Summary Список исполнителей
	// @Tags CRM
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/assignees [get]
	router.Get("/assignees", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY last_name,first_name,username),'[]'::jsonb)
			FROM (SELECT id,username,first_name,last_name,is_active FROM users WHERE role<>'user') s`)
	})
	// @Summary Последние задачи локального агента
	// @Tags CRM
	// @Security BearerAuth
	// @Param agent query int true "ID агента"
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/agent-tasks [get]
	router.Get("/agent-tasks", middleware.ManagerOnly(), func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Query("agent"), 10, 64)
		if err != nil || id < 1 {
			return fiber.ErrBadRequest
		}
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(t) ORDER BY id DESC),'[]'::jsonb)
			FROM (SELECT id,kind,method,status,log,error,created_at FROM agent_tasks WHERE agent_id=$1 ORDER BY id DESC LIMIT 20) t`, id)
	})
}

func RegisterCustomerPortal(router fiber.Router, pool *pgxpool.Pool) {
	// @Summary Мои объекты
	// @Tags Личный кабинет
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /my/sites [get]
	router.Get("/sites", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, installationQuery+`SELECT coalesce(jsonb_agg(to_jsonb(s) ORDER BY name),'[]'::jsonb)
		    FROM (SELECT id,name,address,status,timezone,provider_name FROM enriched WHERE client_id=$1 AND NOT archived) s`, middleware.GetUser(c).ClientID)
	})
	// @Summary Мои заявки
	// @Tags Личный кабинет
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /my/requests [get]
	router.Get("/requests", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(w) ORDER BY id DESC),'[]'::jsonb)
		    FROM (SELECT id,title,status,created_at FROM crm_work WHERE kind='requests' AND client_id=$1 AND created_by=$2) w`, middleware.GetUser(c).ClientID, middleware.GetUser(c).ID)
	})
	// @Summary Подать заявку
	// @Tags Личный кабинет
	// @Security BearerAuth
	// @Param body body customerRequestInput true "Поля запроса"
	// @Success 201 {object} map[string]interface{}
	// @Router /my/requests [post]
	router.Post("/requests", func(c *fiber.Ctx) error {
		user := middleware.GetUser(c)
		if user.ClientID == nil {
			return fiber.ErrForbidden
		}
		var in customerRequestInput
		if err := c.BodyParser(&in); err != nil {
			return fiber.ErrBadRequest
		}
		in.Title = strings.TrimSpace(in.Title)
		if in.Title == "" || len([]rune(in.Title)) > 160 || len(in.Description) > 20000 {
			return fiber.NewError(400, "Укажите тему до 160 символов и описание до 20 000 байт")
		}
		c.Status(fiber.StatusCreated)
		return crmJSON(c, pool, `WITH w AS (INSERT INTO crm_work (kind,title,description,client_id,created_by) VALUES ('requests',$1,$2,$3,$4) RETURNING *),
		    audit AS (INSERT INTO crm_activity(work_id,actor_id,action,snapshot) SELECT id,$4,'create',to_jsonb(w) FROM w)
		    SELECT jsonb_build_object('id',id,'title',title,'status',status,'created_at',created_at) FROM w`, in.Title, in.Description, user.ClientID, user.ID)
	})
}
