package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/middleware"
	"ata-energo/pkg/valueclouds"
	"ata-energo/services"
	"ata-energo/types"

	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func valueCloudsFail(c *fiber.Ctx, err error) error {
	var upstream *valueclouds.APIError
	if errors.As(err, &upstream) {
		return fiber.NewError(502, upstream.Error())
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fiber.NewError(504, "ValueClouds не ответил вовремя")
	}
	var fieldErrors services.FieldErrors
	var detail *services.DetailError
	var local *fiber.Error
	if errors.As(err, &fieldErrors) || errors.As(err, &detail) || errors.As(err, &local) {
		return fail(c, err)
	}
	if strings.HasPrefix(err.Error(), "ValueClouds:") {
		return fiber.NewError(502, err.Error())
	}
	return fail(c, err)
}

type ValueCloudsHistoryInput struct {
	Mode string `json:"mode"`
	Date string `json:"date"`
}

func RegisterValueClouds(router fiber.Router, pool *pgxpool.Pool, s *services.ValueCloudsService) {
	bounded := func(c *fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.UserContext(), 90*time.Second)
		defer cancel()
		c.SetUserContext(ctx)
		return c.Next()
	}
	accounts := router.Group("/valueclouds/accounts", middleware.AdminOnly(), bounded)
	actor := func(c *fiber.Ctx) context.Context {
		return repositories.WithVendorActor(c.UserContext(), middleware.GetUser(c).ID)
	}
	// @Summary Аккаунты ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts [get]
	accounts.Get("", func(c *fiber.Ctx) error {
		list, err := s.ListAccounts(c.UserContext())
		if err != nil {
			return fail(c, err)
		}
		return c.JSON(list)
	})
	// @Summary Аккаунты ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param body body types.VendorAccountWrite true "Учётные данные"
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts [post]
	accounts.Post("", func(c *fiber.Ctx) error {
		var w types.VendorAccountWrite
		if c.BodyParser(&w) != nil {
			return fiber.ErrBadRequest
		}
		a, err := s.SaveAccount(actor(c), 0, w)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.Status(201).JSON(a)
	})
	// @Summary Аккаунт ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts/{id} [get]
	accounts.Get("/:id", func(c *fiber.Ctx) error {
		id, ok := paramID(c, "id")
		if !ok {
			return fiber.ErrNotFound
		}
		a, _, err := s.Account(c.UserContext(), id)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(a)
	})
	// @Summary Аккаунт ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Param body body types.VendorAccountWrite true "Учётные данные"
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts/{id} [patch]
	accounts.Patch("/:id", func(c *fiber.Ctx) error {
		id, ok := paramID(c, "id")
		if !ok {
			return fiber.ErrNotFound
		}
		var w types.VendorAccountWrite
		if c.BodyParser(&w) != nil {
			return fiber.ErrBadRequest
		}
		a, err := s.SaveAccount(actor(c), id, w)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(a)
	})
	// @Summary Аккаунт ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Success 204
	// @Router /valueclouds/accounts/{id} [delete]
	accounts.Delete("/:id", func(c *fiber.Ctx) error {
		id, ok := paramID(c, "id")
		if !ok {
			return fiber.ErrNotFound
		}
		if err := s.DeleteAccount(actor(c), id); err != nil {
			return valueCloudsFail(c, err)
		}
		return c.SendStatus(204)
	})
	// @Summary Проверить подключение ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts/{id}/test [post]
	accounts.Post("/:id/test", func(c *fiber.Ctx) error {
		id, ok := paramID(c, "id")
		if !ok {
			return fiber.ErrNotFound
		}
		if err := s.TestAccount(c.UserContext(), id); err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(fiber.Map{"ok": true})
	})
	// @Summary Устройства аккаунта ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts/{id}/devices [get]
	accounts.Get("/:id/devices", func(c *fiber.Ctx) error {
		id, ok := paramID(c, "id")
		if !ok {
			return fiber.ErrNotFound
		}
		cl, err := s.ActiveClient(c.UserContext(), id)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		devices, err := cl.Devices(c.UserContext())
		if err != nil {
			return valueCloudsFail(c, err)
		}
		out := []fiber.Map{}
		for _, d := range devices {
			out = append(out, fiber.Map{"key": d.Key(), "device": d})
		}
		return c.JSON(out)
	})
	// @Summary Импортировать устройство ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Param body body services.ValueCloudsImportRequest true "Устройство и клиент"
	// @Success 200 {object} map[string]interface{}
	// @Router /valueclouds/accounts/{id}/import [post]
	accounts.Post("/:id/import", func(c *fiber.Ctx) error {
		id, ok := paramID(c, "id")
		if !ok {
			return fiber.ErrNotFound
		}
		var r services.ValueCloudsImportRequest
		if c.BodyParser(&r) != nil {
			return fiber.ErrBadRequest
		}
		m, err := s.Import(c.UserContext(), id, r)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.Status(201).JSON(m)
	})
	sites := router.Group("/crm/valueclouds/sites", middleware.StaffOnly(), bounded)
	target := func(c *fiber.Ctx) (*valueclouds.Client, []valueclouds.Device, error) {
		id, err := strconv.ParseInt(strings.TrimPrefix(c.Params("id"), "s"), 10, 64)
		if err != nil || !strings.HasPrefix(c.Params("id"), "s") {
			return nil, nil, fiber.ErrNotFound
		}
		var accountID int64
		var externalID string
		err = pool.QueryRow(c.UserContext(), `SELECT s.credential_id,s.external_id FROM provider_sites s
   JOIN vendor_accounts a ON a.id=s.credential_id JOIN providers p ON p.code=s.provider_code
   WHERE s.id=$1 AND s.provider_code='valueclouds' AND a.vendor=s.provider_code AND NOT s.archived AND a.is_active AND p.enabled`, id).Scan(&accountID, &externalID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, fiber.ErrNotFound
		}
		if err != nil {
			return nil, nil, err
		}
		return s.SiteDevices(c.UserContext(), accountID, externalID)
	}
	deviceTarget := func(c *fiber.Ctx) (*valueclouds.Client, *valueclouds.Device, error) {
		cl, devices, err := target(c)
		if err != nil {
			return nil, nil, err
		}
		key, decodeErr := url.PathUnescape(c.Params("device"))
		if decodeErr != nil {
			return nil, nil, fiber.ErrBadRequest
		}
		for _, d := range devices {
			if d.Key() == key {
				return cl, &d, nil
			}
		}
		return nil, nil, fiber.ErrNotFound
	}
	// @Summary Телеметрия станции ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/valueclouds/sites/{id}/telemetry [get]
	sites.Get("/:id/telemetry", func(c *fiber.Ctx) error {
		cl, devices, err := target(c)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		items := []any{}
		readings := []any{}
		warnings := []string{}
		flows := []map[string]any{}
		var updated string
		for _, d := range devices {
			items = append(items, vcDeviceView(d))
			snapshot, err := cl.ReadSnapshot(c.UserContext(), d)
			if err != nil {
				warnings = append(warnings, "Не удалось получить измерения "+d.SN)
				continue
			}
			for _, warning := range snapshot.Warnings {
				warnings = append(warnings, d.SN+": "+warning)
			}
			if snapshot.Latest != nil {
				readings = append(readings, vcReading(d, snapshot.Latest))
			}
			if snapshot.Payload == nil {
				continue
			}
			flows = append(flows, snapshot.Payload)
			// A station's freshness is limited by its oldest contributing device.
			if ts, _ := snapshot.Payload["timestamp"].(string); updated == "" || ts < updated {
				updated = ts
			}
		}
		if len(devices) > 0 && len(readings) == 0 && len(flows) == 0 {
			return fiber.NewError(502, "Не удалось получить данные ValueClouds")
		}
		var first map[string]any
		if len(flows) > 0 {
			first = flows[0]
		}
		return c.JSON(fiber.Map{"station": nil, "latest": fiber.Map{"lastUpdateTime": updated}, "flow": vcCompleteFlow(flows, len(devices)), "inverter_flow": first, "devices": items, "readings": readings, "warnings": warnings})
	})
	// @Summary Показания инвертора ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Param device path string true "Ключ устройства из каталога"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/valueclouds/sites/{id}/devices/{device} [get]
	sites.Get("/:id/devices/:device", func(c *fiber.Ctx) error {
		cl, d, err := deviceTarget(c)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		snapshot, err := cl.ReadSnapshot(c.UserContext(), *d)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		latest := snapshot.Latest
		if latest == nil {
			latest = &valueclouds.LastData{}
		}
		warnings := snapshot.Warnings
		flow := snapshot.Payload
		if flow == nil {
			flow = map[string]any{}
		}
		view := vcDeviceView(*d)
		var metadata fiber.Map
		if info, infoErr := cl.DeviceInfo(c.UserContext(), *d); infoErr != nil {
			warnings = append(warnings, "Не удалось обновить сведения об устройстве")
		} else {
			loggerType := info.TypeName
			loggerFirmware := ""
			if info.Collector != nil {
				if info.Collector.TypeName != "" {
					loggerType = info.Collector.TypeName
				}
				loggerFirmware = info.Collector.Firmware
			}
			metadata = fiber.Map{"name": info.Name, "serial": info.Serial, "pn": info.PN, "sn": info.SN, "address": info.Address, "code": info.Code, "logger_type": loggerType, "firmware": info.Firmware, "logger_firmware": loggerFirmware, "utc_offset_seconds": info.Timezone}
		}
		active := []fiber.Map{}
		if parameters, paramsErr := cl.DeviceParameters(c.UserContext(), *d); paramsErr != nil {
			warnings = append(warnings, "Не удалось обновить активные параметры")
		} else {
			sort.SliceStable(parameters, func(i, j int) bool { return parameters[i].Order < parameters[j].Order })
			for _, p := range parameters {
				active = append(active, vcPoint(p.Parameter, p.Name, string(p.Value), p.Unit, "", p.Updated))
			}
		}
		fields := []valueclouds.ChartField{}
		if config, configErr := cl.DeviceChartFields(c.UserContext(), *d); configErr != nil {
			warnings = append(warnings, "Не удалось обновить порядок параметров")
		} else {
			fields = config
			sort.SliceStable(fields, func(i, j int) bool { return fields[i].Order < fields[j].Order })
		}
		return c.JSON(fiber.Map{"device": view, "metadata": metadata, "reading": vcReading(*d, latest), "flow": flow, "active_parameters": active, "chart_fields": fields, "warnings": warnings, "collected_at": snapshot.CollectedAt, "live": snapshot.Payload != nil})
	})
	// @Summary Аварии инвертора ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Param device path string true "Ключ устройства из каталога"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/valueclouds/sites/{id}/devices/{device}/alerts [get]
	sites.Get("/:id/devices/:device/alerts", func(c *fiber.Ctx) error {
		cl, d, err := deviceTarget(c)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		alerts, err := vcAlerts(c.UserContext(), s, cl, []valueclouds.Device{*d})
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(fiber.Map{"alertList": alerts})
	})
	// @Summary Аварии станции ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/valueclouds/sites/{id}/alerts [get]
	sites.Get("/:id/alerts", func(c *fiber.Ctx) error {
		cl, devices, err := target(c)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		alerts, err := vcAlerts(c.UserContext(), s, cl, devices)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(fiber.Map{"stationAlertItems": alerts})
	})
	// @Summary История станции ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Param mode query string true "D, M, Y, T"
	// @Param date query string false "Дата выбранного периода"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/valueclouds/sites/{id}/history [get]
	sites.Get("/:id/history", func(c *fiber.Ctx) error {
		cl, devices, err := target(c)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		points, err := vcHistory(c.UserContext(), s, cl, devices, c.Query("mode"), c.Query("date"))
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(fiber.Map{"stationDataItems": points})
	})
	// @Summary История инвертора ValueClouds
	// @Tags ValueClouds
	// @Security BearerAuth
	// @Param id path string true "ID аккаунта или объекта"
	// @Param device path string true "Ключ устройства из каталога"
	// @Param body body ValueCloudsHistoryInput true "Период"
	// @Success 200 {object} map[string]interface{}
	// @Router /crm/valueclouds/sites/{id}/devices/{device}/history [post]
	sites.Post("/:id/devices/:device/history", func(c *fiber.Ctx) error {
		cl, d, err := deviceTarget(c)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		var input ValueCloudsHistoryInput
		if c.BodyParser(&input) != nil {
			return fiber.ErrBadRequest
		}
		points, err := vcHistory(c.UserContext(), s, cl, []valueclouds.Device{*d}, input.Mode, input.Date)
		if err != nil {
			return valueCloudsFail(c, err)
		}
		return c.JSON(fiber.Map{"dataList": vcInverterHistory(points)})
	})
}
func vcDeviceView(d valueclouds.Device) fiber.Map {
	return fiber.Map{"deviceId": d.Key(), "deviceSn": d.SN, "deviceType": "INVERTER", "name": d.Name, "connectStatus": d.Status, "productId": strconv.Itoa(d.Code)}
}
func vcPoint(key, label, value, unit, group, updated string) fiber.Map {
	if strings.EqualFold(unit, "kW") {
		if n, err := strconv.ParseFloat(value, 64); err == nil {
			value = strconv.FormatFloat(n*1000, 'f', -1, 64)
			unit = "W"
		}
	}
	return fiber.Map{"key": key, "label": label, "value": value, "unit": unit, "group": group, "updated": updated}
}
func vcReading(d valueclouds.Device, raw any) fiber.Map {
	latest, ok := raw.(*valueclouds.LastData)
	if !ok {
		latest = &valueclouds.LastData{}
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, latest)
	}
	points := []any{}
	groups := []struct{ source, target string }{{"gd_", "grid"}, {"sy_", "basic"}, {"pv_", "pv"}, {"bt_", "battery"}, {"bc_", "load"}}
	known := map[string]bool{}
	for _, group := range groups {
		known[group.source] = true
	}
	unknown := []string{}
	for source := range latest.Groups {
		if !known[source] {
			unknown = append(unknown, source)
		}
	}
	sort.Strings(unknown)
	for _, source := range unknown {
		groups = append(groups, struct{ source, target string }{source, "basic"})
	}
	for _, group := range groups {
		for _, p := range latest.Groups[group.source] {
			point := vcPoint(p.ID, p.Name, string(p.Value), p.Unit, group.target, "")
			point["charts"] = p.Charts
			point["source_group"] = group.source
			points = append(points, point)
		}
	}
	state := 0
	if d.Online() {
		state = 1
		if d.Status == 2 || d.Status == 4 {
			state = 2
		}
	}
	return fiber.Map{"deviceSn": d.SN, "deviceType": "INVERTER", "deviceState": state, "collectionTime": latest.Timestamp, "dataList": points}
}

// vcSumFlows aggregates only sections present on every contributing inverter.
// Missing power is unknown, never an implicit zero.
func vcSumFlows(flows []map[string]any) map[string]any {
	out := map[string]any{}
	if len(flows) == 0 {
		return out
	}
	for _, section := range []string{"pv", "grid", "battery", "load", "generator"} {
		total := 0.0
		complete := true
		for _, flow := range flows {
			value := flow[section]
			if section == "pv" {
				parts, _ := value.([]any)
				if len(parts) != 1 {
					complete = false
					break
				}
				value = parts[0]
			}
			part, _ := value.(map[string]any)
			n, ok := part["power"].(float64)
			if !ok {
				complete = false
				break
			}
			total += n
		}
		if !complete {
			continue
		}
		part := map[string]any{"power": total}
		if section == "pv" {
			out[section] = []any{part}
		} else {
			out[section] = part
		}
	}
	// Capacity is unknown; SOC cannot be averaged across batteries.
	if len(flows) == 1 {
		if battery, ok := flows[0]["battery"]; ok {
			out["battery"] = battery
		}
	}
	return out
}
func vcAlerts(ctx context.Context, s *services.ValueCloudsService, cl *valueclouds.Client, devices []valueclouds.Device) ([]any, error) {
	out := []any{}
	for _, d := range devices {
		for page := 1; page <= 500; page++ {
			raw, err := s.CloudData(ctx, cl, d, "alerts", "", page)
			if err != nil {
				return nil, err
			}
			data, _ := raw.(map[string]any)
			items, _ := data["items"].([]any)
			total, ok := data["total"].(float64)
			if !ok {
				return nil, errors.New("ValueClouds: некорректный список аварий")
			}
			for _, item := range items {
				p, _ := item.(map[string]any)
				out = append(out, vcAlert(d, p))
			}
			if page*100 >= int(total) {
				break
			}
			if len(items) == 0 || page == 500 {
				return nil, errors.New("ValueClouds: неполный список аварий")
			}
		}
	}
	return out, nil
}
func vcText(p map[string]any, keys ...string) string {
	for _, k := range keys {
		if text, ok := p[k].(string); ok && text != "" {
			return text
		}
	}
	return ""
}
func vcEpoch(p map[string]any) int64 {
	for _, key := range []string{"gts", "warnTime", "createTime", "timestamp", "time"} {
		if n, ok := p[key].(float64); ok {
			if n > 1e12 {
				n /= 1000
			}
			return int64(n)
		}
		if text, ok := p[key].(string); ok {
			if date, err := time.Parse(time.RFC3339, text); err == nil {
				return date.Unix()
			}
		}
	}
	return 0
}

func vcHistory(ctx context.Context, s *services.ValueCloudsService, cl *valueclouds.Client, devices []valueclouds.Device, mode, date string) ([]map[string]any, error) {
	if mode != "D" && mode != "M" && mode != "Y" && mode != "T" {
		return nil, services.Field("mode", "Выберите период D, M, Y или T")
	}
	zone, _ := time.LoadLocation("Asia/Bishkek")
	now := time.Now().In(zone)
	if mode != "T" {
		layout := map[string]string{"D": "2006-01-02", "M": "2006-01", "Y": "2006"}[mode]
		start, err := time.ParseInLocation(layout, date, zone)
		if err != nil || start.Year() < 1970 || start.After(now) {
			return nil, services.Field("date", "Укажите корректную дату без перехода в будущее")
		}
	}
	combined := map[string]map[string]any{}
	contributors := map[string]map[string]map[string]bool{}
	gaps := map[string]map[string]bool{}
	for _, d := range devices {
		years := []int{now.Year()}
		if mode == "T" {
			if created, err := time.Parse("2006-01-02 15:04:05", d.Created); err == nil && created.Year() <= now.Year() {
				years = nil
				for y := max(created.Year(), now.Year()-30); y <= now.Year(); y++ {
					years = append(years, y)
				}
			}
		}
		for _, year := range years {
			requestMode, requestDate := mode, date
			if mode == "T" {
				requestMode = "Y"
				requestDate = strconv.Itoa(year)
			}
			raw, err := s.CloudData(ctx, cl, d, requestMode, requestDate, 1)
			if err != nil {
				return nil, err
			}
			points, err := valueclouds.HistoryPoints(raw, requestMode, zone)
			if err != nil {
				return nil, err
			}
			for _, point := range points {
				key := point["timeStamp"].(string)
				if mode == "T" {
					key = strconv.Itoa(year)
				}
				row := combined[key]
				if row == nil {
					row = map[string]any{"timeStamp": point["timeStamp"], "year": point["year"], "month": point["month"], "day": point["day"]}
					if mode == "T" {
						row["month"] = nil
						row["day"] = nil
					}
					combined[key] = row
				}
				for field, value := range point {
					if field == "timeStamp" || field == "year" || field == "month" || field == "day" {
						continue
					}
					n, ok := value.(float64)
					if !ok {
						if gaps[key] == nil {
							gaps[key] = map[string]bool{}
						}
						gaps[key][field] = true
						if _, exists := row[field]; !exists {
							row[field] = nil
						}
						continue
					}
					total, _ := row[field].(float64)
					row[field] = total + n
					if contributors[key] == nil {
						contributors[key] = map[string]map[string]bool{}
					}
					if contributors[key][field] == nil {
						contributors[key][field] = map[string]bool{}
					}
					contributors[key][field][d.Key()] = true
				}
			}
		}
	}
	keys := make([]string, 0, len(combined))
	for k := range combined {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	result := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		for field := range combined[k] {
			if field == "timeStamp" || field == "year" || field == "month" || field == "day" {
				continue
			}
			if len(contributors[k][field]) != len(devices) || gaps[k][field] {
				combined[k][field] = nil
			}
		}
		result = append(result, combined[k])
	}
	return result, nil
}
func vcInverterHistory(points []map[string]any) []any {
	out := []any{}
	fields := map[string]string{"generationPower": "pv_output_power", "consumptionPower": "load_active_power", "gridPower": "grid_active_power", "batteryPower": "battery_active_power", "generationValue": "Production", "consumptionValue": "Consumption", "purchaseValue": "ElectricityPurchasing", "gridValue": "GridFeed-in", "chargeValue": "ChargingCapacity", "dischargeValue": "DischargingCapacity"}
	for _, point := range points {
		items := []any{}
		for field, key := range fields {
			if value, ok := point[field]; ok {
				unit := "kWh"
				if strings.HasSuffix(field, "Power") {
					unit = "W"
				}
				items = append(items, fiber.Map{"key": key, "value": value, "unit": unit})
			}
		}
		out = append(out, fiber.Map{"time": point["timeStamp"], "itemList": items})
	}
	return out
}

func vcCompleteFlow(flows []map[string]any, expected int) map[string]any {
	if len(flows) != expected {
		return map[string]any{}
	}
	var oldest, newest time.Time
	for _, flow := range flows {
		at := valueclouds.PayloadTime(flow)
		if at.IsZero() {
			return map[string]any{}
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
		if at.After(newest) {
			newest = at
		}
	}
	if newest.Sub(oldest) > valueclouds.MaxSourceSkew {
		return map[string]any{}
	}
	return vcSumFlows(flows)
}

func vcAlert(d valueclouds.Device, p map[string]any) fiber.Map {
	timestamp := vcEpoch(p)
	id := vcText(p, "id")
	if id == "" {
		id = d.Key() + ":" + fmt.Sprint(p["code"]) + ":" + strconv.FormatInt(timestamp, 10)
	}
	title := vcText(p, "desc", "warnName", "warningName", "description", "content")
	if title == "" {
		title = "Предупреждение инвертора " + d.SN
	}
	status := 1
	if handled, _ := p["handle"].(bool); handled {
		status = 0
	}
	return fiber.Map{"alertId": id, "alertName": title, "description": vcText(p, "advice", "description"), "alertStartTime": timestamp, "status": status}
}
