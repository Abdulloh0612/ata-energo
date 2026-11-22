package api

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ata-energo/middleware"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Existing local modems remain visible as installations. Cloud catalog rows are
// stored independently, with an explicit client assignment when one is made.
const installationQuery = `WITH catalog AS (
	SELECT 's'||s.id::text AS id,s.id AS catalog_id,s.provider_code,s.external_id,s.credential_id,
		coalesce(s.client_id,m.owner_id) AS client_id,s.name,s.address,s.timezone,s.latitude,s.longitude,s.status,
		s.archived,s.last_synced_at,m.modem_ids
	FROM provider_sites s
	LEFT JOIN LATERAL (
		SELECT CASE WHEN count(DISTINCT client_id)=1 THEN min(client_id) END AS owner_id,
			coalesce(array_agg(id ORDER BY id),ARRAY[]::bigint[]) AS modem_ids
		FROM modems
		WHERE s.provider_code='deye' AND connection->>'protocol'='deye_cloud'
			AND connection->>'account_id'=s.credential_id::text AND connection->>'station_id'=s.external_id
	) m ON true
), legacy AS (
	SELECT 'm'||m.id::text AS id,NULL::bigint AS catalog_id,
		CASE WHEN m.connection->>'protocol'='deye_cloud' THEN 'deye' ELSE 'local' END AS provider_code,
		m.serial AS external_id,a.id AS credential_id,m.client_id,
		coalesce(nullif(m.site,''),nullif(m.name,''),m.serial) AS name,c.address,'Asia/Bishkek' AS timezone,
		NULL::double precision AS latitude,NULL::double precision AS longitude,
		CASE m.status WHEN 'online' THEN 'normal' ELSE 'offline' END AS status,false AS archived,
		m.last_seen AS last_synced_at,ARRAY[m.id] AS modem_ids
	FROM modems m JOIN clients c ON c.id=m.client_id
	LEFT JOIN vendor_accounts a ON m.connection->>'protocol'='deye_cloud' AND a.id::text=m.connection->>'account_id'
	WHERE NOT EXISTS(SELECT 1 FROM catalog k WHERE m.id=ANY(k.modem_ids))
), installations AS (
	SELECT * FROM catalog UNION ALL SELECT * FROM legacy
), enriched AS (
	SELECT i.*,p.name AS provider_name,p.enabled,p.mock,c.name AS client_name,
		(SELECT COUNT(*) FROM devices d WHERE d.modem_id=ANY(i.modem_ids)) AS device_count
	FROM installations i JOIN providers p ON p.code=i.provider_code LEFT JOIN clients c ON c.id=i.client_id
) `

var providerCode = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,29}$`)

type siteClientInput struct {
	ClientID int64 `json:"client_id"`
}

type providerInput struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Adapter string `json:"adapter" enums:"local,eybond"`
}

type providerState struct {
	Enabled *bool `json:"enabled"`
}

func RegisterProviders(router fiber.Router, pool *pgxpool.Pool, catalog *services.ProviderCatalog, deyeService *services.DeyeService) {
	// @Summary Объекты станций
	// @Tags CRM · Объекты
	// @Security BearerAuth
	// @Param provider query string false "Коды провайдеров через запятую, например deye,local"
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/sites [get]
	router.Get("/sites", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return crmJSON(c, pool, installationQuery+`SELECT coalesce(jsonb_agg(to_jsonb(e) ORDER BY name,id),'[]'::jsonb)
			FROM enriched e WHERE ($1='' OR provider_code=ANY(string_to_array($1,',')))`, c.Query("provider"))
	})
	// @Summary Карточка объекта и его устройства
	// @Tags CRM · Объекты
	// @Security BearerAuth
	// @Param id path string true "ID из списка объектов"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/sites/{id} [get]
	router.Get("/sites/:id", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return crmJSON(c, pool, installationQuery+`SELECT to_jsonb(e) || jsonb_build_object('devices',
			(SELECT coalesce(jsonb_agg(jsonb_build_object('id',d.id,'name',d.name,'serial_number',d.serial_number,
			'status',d.status,'power',d.last_data->'power') ORDER BY d.id),'[]'::jsonb) FROM devices d WHERE d.modem_id=ANY(e.modem_ids)))
			FROM enriched e WHERE e.id=$1`, c.Params("id"))
	})
	router.Get("/sites/:id/telemetry", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return siteTelemetry(c, pool, deyeService)
	})
	router.Get("/sites/:id/history", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return siteCloudHistory(c, pool, deyeService)
	})
	router.Get("/sites/:id/alerts", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return siteCloudAlerts(c, pool, deyeService)
	})
	// @Summary Привязать объект к клиенту
	// @Tags CRM · Объекты
	// @Security BearerAuth
	// @Param id path string true "ID из списка объектов"
	// @Param body body siteClientInput true "Поля запроса"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/sites/{id}/client [put]
	router.Put("/sites/:id/client", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		u := middleware.GetUser(c)
		if !u.IsManager() && u.Role != types.RoleCRMManager {
			return fiber.ErrForbidden
		}
		id, err := strconv.ParseInt(strings.TrimPrefix(c.Params("id"), "s"), 10, 64)
		if !strings.HasPrefix(c.Params("id"), "s") || err != nil {
			return fiber.ErrNotFound
		}
		var data siteClientInput
		if err := c.BodyParser(&data); err != nil || data.ClientID <= 0 {
			return fiber.NewError(400, "Выберите клиента")
		}
		return crmJSON(c, pool, installationQuery+`UPDATE provider_sites s SET client_id=$2 FROM enriched e
			WHERE s.id=$1 AND e.catalog_id=s.id AND e.client_id IS NULL AND s.client_id IS NULL AND NOT s.archived AND cardinality(e.modem_ids)=0
			AND EXISTS(SELECT 1 FROM clients c WHERE c.id=$2)
			RETURNING to_jsonb(s)`, id, data.ClientID)
	})
	// @Summary Провайдеры и число объектов
	// @Tags CRM · Объекты
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/providers [get]
	router.Get("/providers", middleware.StaffOnly(), func(c *fiber.Ctx) error {
		return crmJSON(c, pool, installationQuery+`SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY name),'[]'::jsonb)
			FROM (SELECT p.*, (SELECT count(*) FROM enriched e WHERE e.provider_code=p.code AND NOT e.archived) AS sites_count,
			(SELECT coalesce(sum(device_count),0) FROM enriched e WHERE e.provider_code=p.code AND NOT e.archived) AS devices_count
			FROM providers p) x`)
	})
	integrations := router.Group("/integrations", middleware.AdminOnly())
	// @Summary Журнал изменений учётных данных
	// @Tags CRM · Интеграции
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/integrations/activity [get]
	integrations.Get("/activity", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY id DESC),'[]'::jsonb) FROM
			(SELECT e.*,u.username AS actor_name FROM provider_credential_events e LEFT JOIN users u ON u.id=e.actor_id ORDER BY e.id DESC LIMIT 100) x`)
	})
	// @Summary Состояние подключённых аккаунтов
	// @Tags CRM · Интеграции
	// @Security BearerAuth
	// @Success 200 {array} map[string]interface{}
	// @Router /crm/integrations/accounts [get]
	integrations.Get("/accounts", func(c *fiber.Ctx) error {
		return crmJSON(c, pool, `SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY name),'[]'::jsonb) FROM
			(SELECT a.id,a.name,a.vendor AS provider_code,a.base_url,a.is_active,a.last_sync_at,a.last_error,
			(a.fields_encrypted IS NOT NULL) has_credentials,s.last_attempt_at,s.last_success_at,s.last_error AS catalog_error
			FROM vendor_accounts a LEFT JOIN provider_catalog_sync s ON s.account_id=a.id) x`)
	})
	// @Summary Создать провайдера
	// @Tags CRM · Интеграции
	// @Security BearerAuth
	// @Param body body providerInput true "Поля запроса"
	// @Success 201 {object} map[string]string
	// @Router /crm/integrations/providers [post]
	integrations.Post("/providers", func(c *fiber.Ctx) error {
		var input providerInput
		if err := c.BodyParser(&input); err != nil {
			return fiber.ErrBadRequest
		}
		input.Name = strings.TrimSpace(input.Name)
		if !providerCode.MatchString(input.Code) || input.Name == "" || len([]rune(input.Name)) > 100 ||
			(input.Adapter != "local" && input.Adapter != "eybond") {
			return fiber.NewError(400, "Проверьте код, название и адаптер провайдера")
		}
		var code string
		err := pool.QueryRow(c.UserContext(), `INSERT INTO providers(code,name,adapter,enabled,mock) VALUES($1,$2,$3,true,$3='eybond')
			ON CONFLICT DO NOTHING RETURNING code`, input.Code, input.Name, input.Adapter).Scan(&code)
		if errors.Is(err, pgx.ErrNoRows) {
			return fiber.NewError(409, "Код провайдера уже используется")
		}
		if err != nil {
			return err
		}
		return c.Status(201).JSON(fiber.Map{"code": code})
	})
	// @Summary Включить или выключить провайдера
	// @Tags CRM · Интеграции
	// @Security BearerAuth
	// @Param code path string true "Код"
	// @Param body body providerState true "Поля запроса"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/integrations/providers/{code} [put]
	integrations.Put("/providers/:code", func(c *fiber.Ctx) error {
		var input providerState
		if err := c.BodyParser(&input); err != nil || input.Enabled == nil {
			return fiber.ErrBadRequest
		}
		return crmJSON(c, pool, `UPDATE providers SET enabled=$2 WHERE code=$1 AND adapter<>'local' RETURNING to_jsonb(providers)`, c.Params("code"), *input.Enabled)
	})
	// @Summary Синхронизировать каталог аккаунта
	// @Tags CRM · Интеграции
	// @Security BearerAuth
	// @Param id path int true "ID записи"
	// @Success 200 {object} map[string]bool
	// @Router /crm/integrations/accounts/{id}/sync [post]
	integrations.Post("/accounts/:id/sync", func(c *fiber.Ctx) error {
		id, err := strconv.ParseInt(c.Params("id"), 10, 64)
		if err != nil {
			return fiber.ErrNotFound
		}
		ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
		defer cancel()
		if err := catalog.Sync(ctx, id); err != nil {
			return fail(c, err)
		}
		return c.JSON(fiber.Map{"ok": true})
	})
}
