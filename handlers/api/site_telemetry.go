package api

import (
	"ata-energo/pkg/deye"
	"ata-energo/services"
	"context"
	"errors"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
	"strings"
	"time"
)

// siteTelemetry reads catalog telemetry independently of local client ownership.
// @Summary Текущие данные и оборудование станции Deye
// @Tags CRM · Объекты
// @Security BearerAuth
// @Param id path string true "ID объекта из каталога"
// @Success 200 {object} map[string]interface{}
// @Router /crm/sites/{id}/telemetry [get]
func siteTelemetry(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) error {
	client, stationID, _, err := cloudStationTarget(c, pool, service)
	if err != nil {
		return fail(c, err)
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 60*time.Second)
	defer cancel()
	warnings := []string{}
	detail, detailErr := client.StationDetail(ctx, stationID)
	if detailErr != nil {
		warnings = append(warnings, "Не удалось получить паспорт станции")
	}
	var station *deye.Station
	if detailErr == nil {
		station = detail.Station
	}
	latest, latestErr := client.StationLatest(ctx, stationID)
	if latestErr != nil {
		warnings = append(warnings, "Не удалось получить показатели станции")
		latest = nil
	}
	items := []deye.DeviceListItem{}
	for page := 1; ; page++ {
		reply, err := client.StationDevices(ctx, deye.StationDeviceRequest{StationIDs: []int64{stationID}, Page: page, Size: 200})
		if err != nil {
			if latestErr != nil {
				return deyeFail(c, err)
			}
			warnings = append(warnings, "Не удалось получить полный список оборудования")
			break
		}
		items = append(items, reply.DeviceListItems...)
		if len(items) >= reply.Total || len(reply.DeviceListItems) == 0 {
			break
		}
		if page >= 500 {
			return fiber.NewError(502, "Превышен предел страниц оборудования")
		}
	}
	readings := []deye.DeviceLatestData{}
	for start := 0; start < len(items); start += deye.MaxLatestBatch {
		sns := []string{}
		for _, item := range items[start:min(start+deye.MaxLatestBatch, len(items))] {
			sns = append(sns, item.DeviceSn)
		}
		reply, err := client.DeviceLatest(ctx, deye.DeviceLatestRequest{DeviceList: sns})
		if err != nil {
			warnings = append(warnings, "Не удалось получить все измерения оборудования")
			break
		}
		readings = append(readings, reply.DeviceDataList...)
	}
	var inverterFlow map[string]any
	for i := range readings {
		if readings[i].DeviceType == "INVERTER" && readings[i].DeviceState != deye.DeviceStateOffline {
			inverterFlow = deye.BuildModemPayload("", &readings[i], nil, deye.MapOptions{})
			break
		}
	}
	return c.JSON(fiber.Map{"inverter_flow": inverterFlow, "station": station, "latest": latest, "devices": items, "readings": readings, "warnings": warnings,
		"flow": deye.BuildModemPayload("", nil, latest, deye.MapOptions{})})
}

func cloudStationTarget(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) (*deye.Client, int64, *time.Location, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(c.Params("id"), "s"), 10, 64)
	if err != nil || !strings.HasPrefix(c.Params("id"), "s") {
		return nil, 0, nil, fiber.ErrNotFound
	}
	var accountID int64
	var externalID, timezone string
	err = pool.QueryRow(c.UserContext(), `SELECT s.credential_id,s.external_id,s.timezone
 FROM provider_sites s JOIN vendor_accounts a ON a.id=s.credential_id
 JOIN providers p ON p.code=s.provider_code
 WHERE s.id=$1 AND s.provider_code='deye' AND NOT s.archived AND a.is_active AND p.enabled`, id).Scan(&accountID, &externalID, &timezone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, nil, fiber.ErrNotFound
	}
	if err != nil {
		return nil, 0, nil, err
	}
	stationID, err := strconv.ParseInt(externalID, 10, 64)
	if err != nil {
		return nil, 0, nil, fiber.NewError(502, "Некорректный номер станции в каталоге")
	}
	_, client, err := service.Account(c.UserContext(), accountID)
	if err != nil {
		return nil, 0, nil, err
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil {
		zone = time.UTC
	}
	return client, stationID, zone, nil
}

// siteCloudHistory reads station history in the station's time zone.
// @Summary История мощности и энергии облачной станции
// @Tags CRM · Объекты
// @Security BearerAuth
// @Param id path string true "ID объекта"
// @Param mode query string true "D — сутки, M — месяц, Y — год, T — всё время" Enums(D,M,Y,T)
// @Param date query string false "YYYY-MM-DD, YYYY-MM или YYYY"
// @Success 200 {object} deye.StationHistoryResponse
// @Router /crm/sites/{id}/history [get]
func siteCloudHistory(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) error {
	client, stationID, zone, err := cloudStationTarget(c, pool, service)
	if err != nil {
		return fail(c, err)
	}
	now := time.Now().In(zone)
	mode := c.Query("mode")
	layouts := map[string]string{"D": "2006-01-02", "M": "2006-01", "Y": "2006"}
	layout, valid := layouts[mode]
	if !valid && mode != "T" {
		return fiber.NewError(400, "Выберите период D, M, Y или T")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	req := deye.StationHistoryRequest{StationID: stationID}
	if mode == "T" {
		detail, err := client.StationDetail(ctx, stationID)
		if err != nil {
			return deyeFail(c, err)
		}
		firstYear := now.Year()
		if detail.Station != nil {
			if seconds, ok := detail.Station.StartOperatingTime.Float(); ok && seconds > 0 {
				if seconds > 1e12 {
					seconds /= 1000
				}
				firstYear = time.Unix(int64(seconds), 0).In(zone).Year()
			}
		}
		if firstYear < 1970 || firstYear > now.Year() {
			firstYear = now.Year()
		}
		req.Granularity = 4
		req.StartAt = strconv.Itoa(firstYear)
		req.EndAt = strconv.Itoa(now.Year())
	} else {
		start, err := time.ParseInLocation(layout, c.Query("date"), zone)
		if err != nil || start.Year() < 1970 || start.After(now) {
			return fiber.NewError(400, "Укажите корректную дату без перехода в будущее")
		}
		if mode == "D" {
			end := start.AddDate(0, 0, 1).Add(-time.Second)
			if end.After(now) {
				end = now
			}
			reply, err := client.StationHistoryPower(ctx, deye.StationHistoryPowerRequest{StationID: stationID, StartTimestamp: start.Unix(), EndTimestamp: end.Unix()})
			if err != nil {
				return deyeFail(c, err)
			}
			return c.JSON(reply)
		}

		// Deye daily and monthly energy ranges exclude the upper boundary.
		end := start.AddDate(1, 0, 0)
		req.Granularity = 3
		if mode == "M" {
			end = start.AddDate(0, 1, 0)
			req.Granularity = 2
			limit := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, zone).AddDate(0, 0, 1)
			if end.After(limit) {
				end = limit
			}
		} else {
			limit := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone).AddDate(0, 1, 0)
			if end.After(limit) {
				end = limit
			}
		}
		req.StartAt = start.Format("2006-01-02")
		req.EndAt = end.Format("2006-01-02")
		if mode == "Y" {
			req.StartAt = start.Format("2006-01")
			req.EndAt = end.Format("2006-01")
		}
	}
	reply, err := client.StationHistory(ctx, req)
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(reply)
}

// siteCloudAlerts reads the station's recent alerts.
// @Summary Аварии облачной станции за последние 30 дней
// @Tags CRM · Объекты
// @Security BearerAuth
// @Param id path string true "ID объекта"
// @Success 200 {object} deye.StationAlertListResponse
// @Router /crm/sites/{id}/alerts [get]
func siteCloudAlerts(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) error {
	client, stationID, _, err := cloudStationTarget(c, pool, service)
	if err != nil {
		return fail(c, err)
	}
	now := time.Now()
	reply, err := client.StationAlertList(c.UserContext(), deye.StationAlertListRequest{StationID: stationID, StartTimestamp: now.AddDate(0, 0, -30).Unix(), EndTimestamp: now.Unix(), Page: 1, Size: 200})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(reply)
}
