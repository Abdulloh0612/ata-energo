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

type equipmentDetails struct {
	Model         string   `json:"model"`
	NominalWatts  *float64 `json:"nominal_watts"`
	InstalledOn   *string  `json:"installed_on"`
	ServicedOn    *string  `json:"serviced_on"`
	ServiceDueOn  *string  `json:"service_due_on"`
	WarrantyUntil *string  `json:"warranty_until"`
	Maintenance   bool     `json:"maintenance"`
}

const conditionQuery = `SELECT d.id AS device_id,d.client_id, e.model,e.nominal_watts,e.installed_on,e.serviced_on,e.service_due_on,e.warranty_until,e.maintenance,s.low_power_percent,
	(e.service_due_on <= CURRENT_DATE) AS service_due, (e.warranty_until >= CURRENT_DATE) AS on_warranty,
	CASE
		WHEN nullif(d.last_data->>'error','') IS NOT NULL AND d.status='online' THEN 'problem'
		WHEN e.maintenance THEN 'maintenance'
		WHEN d.status<>'online' THEN 'offline'
		WHEN e.nominal_watts IS NOT NULL AND jsonb_typeof(d.last_data->'power')='number'
			AND (d.device_type NOT IN ('panel','inverter') OR (now() AT TIME ZONE 'Asia/Bishkek')::time >= '10:00'::time
				AND (now() AT TIME ZONE 'Asia/Bishkek')::time < '15:00'::time)
		  THEN CASE WHEN (d.last_data->>'power')::numeric < e.nominal_watts*s.low_power_percent/100 THEN 'low' ELSE 'normal' END
		ELSE 'normal'
	END AS condition
	FROM devices d LEFT JOIN crm_equipment e ON e.device_id=d.id
	CROSS JOIN crm_settings s`

func registerCRMEquipment(router fiber.Router, pool *pgxpool.Pool) {
	equip := router.Group("/equipment", middleware.StaffOnly())
	equip.Get("/", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY device_id),'[]'::jsonb) FROM (`+conditionQuery+`) x`)
	})
	equip.Get("/:id", func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return fiber.ErrNotFound
		}
		u := middleware.GetUser(c)
		return crmJSON(c, pool, `SELECT to_jsonb(x) || jsonb_build_object('can_edit',
			($2::boolean OR $3::boolean AND EXISTS(SELECT 1 FROM crm_work WHERE device_id=$1 AND assignee_id=$4 AND status<>'closed')))
			FROM (`+conditionQuery+` WHERE d.id=$1) x`, id, u.IsManager(), u.Role == types.RoleTechnician, u.ID)
	})
	equip.Put("/:id", func(c *fiber.Ctx) error {
		u := middleware.GetUser(c)
		if !u.IsManager() && u.Role != types.RoleTechnician {
			return fiber.ErrForbidden
		}
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return fiber.ErrNotFound
		}
		var in equipmentDetails
		if err := c.BodyParser(&in); err != nil {
			return fiber.ErrBadRequest
		}
		in.Model = strings.TrimSpace(in.Model)
		if len([]rune(in.Model)) > 120 || in.NominalWatts != nil && (*in.NominalWatts <= 0 || *in.NominalWatts > 1e9) {
			return fiber.NewError(400, "Проверьте модель и номинальную мощность")
		}
		for _, value := range []*string{in.InstalledOn, in.ServicedOn, in.ServiceDueOn, in.WarrantyUntil} {
			if value == nil {
				continue
			}
			if _, err := time.Parse(time.DateOnly, *value); err != nil {
				return fiber.NewError(400, "Проверьте формат даты")
			}
		}
		if in.ServicedOn != nil && in.ServiceDueOn != nil && *in.ServiceDueOn < *in.ServicedOn {
			return fiber.NewError(400, "Следующее обслуживание должно быть после предыдущего")
		}
		var result int64
		err = pool.QueryRow(c.UserContext(), `INSERT INTO crm_equipment (device_id,model,nominal_watts,installed_on,serviced_on,service_due_on,warranty_until,maintenance)
		SELECT id,$2,$3,$4::text::date,$5::text::date,$6::text::date,$7::text::date,$8 FROM devices d WHERE d.id=$1
		AND ($9::boolean OR EXISTS (SELECT 1 FROM crm_work w WHERE w.device_id=d.id AND w.assignee_id=$10 AND w.status<>'closed'))
		ON CONFLICT (device_id) DO UPDATE SET model=excluded.model,nominal_watts=excluded.nominal_watts,installed_on=excluded.installed_on,
		serviced_on=excluded.serviced_on,service_due_on=excluded.service_due_on,warranty_until=excluded.warranty_until,maintenance=excluded.maintenance
		RETURNING device_id`, id, in.Model, in.NominalWatts, in.InstalledOn, in.ServicedOn, in.ServiceDueOn, in.WarrantyUntil, in.Maintenance, u.IsManager(), u.ID).Scan(&result)
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.ErrNotFound
		}
		if err != nil {
			return err
		}
		return c.JSON(fiber.Map{"device_id": result})
	})
	settings := router.Group("/settings", middleware.AdminOnly())
	settings.Get("/", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT jsonb_build_object('low_power_percent',low_power_percent) FROM crm_settings`)
	})
	settings.Put("/", func(c *fiber.Ctx) error {
		var value struct {
			LowPower int `json:"low_power_percent"`
		}
		if err := c.BodyParser(&value); err != nil || value.LowPower < 1 || value.LowPower > 100 {
			return fiber.NewError(400, "Укажите порог от 1 до 100 процентов")
		}
		return crmJSON(c, pool, `UPDATE crm_settings SET low_power_percent=$1 RETURNING jsonb_build_object('low_power_percent',low_power_percent)`, value.LowPower)
	})
}
