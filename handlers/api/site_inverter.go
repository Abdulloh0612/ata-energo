package api

import (
	"ata-energo/pkg/deye"
	"ata-energo/services"
	"context"
	"github.com/gofiber/fiber/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"strconv"
	"time"
)

// stationInverter resolves the inverter through the station's authorized device list.
func stationInverter(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) (*deye.Client, *deye.DeviceListItem, *time.Location, error) {
	client, stationID, zone, err := cloudStationTarget(c, pool, service)
	if err != nil {
		return nil, nil, nil, err
	}
	id, err := strconv.ParseInt(c.Params("device"), 10, 64)
	if err != nil || id <= 0 {
		return nil, nil, nil, fiber.ErrNotFound
	}
	for page := 1; page <= 500; page++ {
		reply, err := client.StationDevices(c.UserContext(), deye.StationDeviceRequest{StationIDs: []int64{stationID}, Page: page, Size: 200})
		if err != nil {
			return nil, nil, nil, err
		}
		for _, device := range reply.DeviceListItems {
			if device.DeviceID == id && device.DeviceType == "INVERTER" {
				device.StationID = stationID
				return client, &device, zone, nil
			}
		}
		if len(reply.DeviceListItems) == 0 || page*200 >= reply.Total {
			break
		}
	}
	return nil, nil, nil, fiber.ErrNotFound
}

// siteInverterData returns all measurements supplied by Deye for this inverter.
// @Summary Полные показатели инвертора станции
// @Tags CRM · Объекты
// @Security BearerAuth
// @Param id path string true "ID объекта"
// @Param device path int true "ID устройства Deye из списка станции"
// @Success 200 {object} map[string]interface{}
// @Router /crm/sites/{id}/devices/{device} [get]
func siteInverterData(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) error {
	client, device, _, err := stationInverter(c, pool, service)
	if err != nil {
		return deyeFail(c, err)
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 30*time.Second)
	defer cancel()
	reply, err := client.DeviceLatest(ctx, deye.DeviceLatestRequest{DeviceList: []string{device.DeviceSn}, DeviceType: device.DeviceType})
	if err != nil {
		return deyeFail(c, err)
	}
	for _, reading := range reply.DeviceDataList {
		if reading.DeviceSn == device.DeviceSn && reading.DeviceType == device.DeviceType {
			return c.JSON(fiber.Map{"device": device, "reading": reading, "flow": deye.BuildModemPayload(device.DeviceSn, &reading, nil, deye.MapOptions{})})
		}
	}
	return fiber.NewError(502, "Deye не вернул измерения этого инвертора")
}

type inverterHistoryInput struct {
	Mode          string   `json:"mode"`
	Date          string   `json:"date"`
	MeasurePoints []string `json:"measurePoints"`
}

// siteInverterHistory reads selected inverter parameters in its station's time zone.
// @Summary История выбранных параметров инвертора
// @Tags CRM · Объекты
// @Security BearerAuth
// @Param id path string true "ID объекта"
// @Param device path int true "ID устройства Deye"
// @Param body body inverterHistoryInput true "Период и ключи измерений"
// @Success 200 {object} deye.DeviceHistoryResponse
// @Router /crm/sites/{id}/devices/{device}/history [post]
func siteInverterHistory(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) error {
	var input inverterHistoryInput
	if err := c.BodyParser(&input); err != nil {
		return fiber.ErrBadRequest
	}
	layouts := map[string]string{"D": "2006-01-02", "M": "2006-01", "Y": "2006", "T": "2006"}
	layout, valid := layouts[input.Mode]
	if !valid || (input.Mode == "D" && len(input.MeasurePoints) == 0) {
		return fiber.NewError(400, "Выберите период и параметры графика")
	}
	if len(input.MeasurePoints) > 150 {
		return fiber.NewError(400, "Выберите не более 150 параметров")
	}
	client, device, zone, err := stationInverter(c, pool, service)
	if err != nil {
		return deyeFail(c, err)
	}
	now := time.Now().In(zone)
	start, err := time.ParseInLocation(layout, input.Date, zone)
	if err != nil || start.Year() < 1970 || start.After(now) {
		return fiber.NewError(400, "Укажите дату без перехода в будущее")
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), 45*time.Second)
	defer cancel()
	if input.Mode == "D" {
		end := start.AddDate(0, 0, 1).Add(-time.Second)
		if end.After(now) {
			end = now
		}
		reply, err := client.DeviceHistoryRawBatched(ctx, deye.DeviceHistoryRawRequest{DeviceSn: device.DeviceSn, DeviceType: device.DeviceType, StartTimestamp: start.Unix(), EndTimestamp: end.Unix(), MeasurePoints: input.MeasurePoints})
		if err != nil {
			return deyeFail(c, err)
		}
		return c.JSON(reply)
	}
	granularity := 2
	end := start.AddDate(0, 1, 0)
	format := "2006-01-02"
	if input.Mode == "Y" {
		granularity = 3
		end = start.AddDate(1, 0, 0)
		format = "2006-01"
	}
	if input.Mode == "T" {
		detail, err := client.StationDetail(ctx, device.StationID)
		if err != nil {
			return deyeFail(c, err)
		}
		if detail.Station != nil {
			if seconds, ok := detail.Station.StartOperatingTime.Float(); ok && seconds > 0 {
				if seconds > 1e12 {
					seconds /= 1000
				}
				year := time.Unix(int64(seconds), 0).In(zone).Year()
				if year >= 1970 && year <= now.Year() {
					start = time.Date(year, 1, 1, 0, 0, 0, 0, zone)
				}
			}
		}
		granularity = 4
		end = now
		format = "2006"
	}
	if end.After(now) {
		end = now
		if input.Mode == "Y" {
			end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, zone).AddDate(0, 1, 0)
		}
	}
	reply, err := client.DeviceHistory(ctx, deye.DeviceHistoryRequest{DeviceSn: device.DeviceSn, DeviceType: device.DeviceType, Granularity: granularity, StartAt: start.Format(format), EndAt: end.Format(format), MeasurePoints: nil})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(reply)
}

// siteInverterAlerts reads recent alerts for one authorized inverter.
// @Summary Аварии инвертора за последние 30 дней
// @Tags CRM · Объекты
// @Security BearerAuth
// @Param id path string true "ID объекта"
// @Param device path int true "ID устройства Deye"
// @Success 200 {object} deye.DeviceAlertListResponse
// @Router /crm/sites/{id}/devices/{device}/alerts [get]
func siteInverterAlerts(c *fiber.Ctx, pool *pgxpool.Pool, service *services.DeyeService) error {
	client, device, _, err := stationInverter(c, pool, service)
	if err != nil {
		return deyeFail(c, err)
	}
	now := time.Now()
	reply, err := client.DeviceAlertList(c.UserContext(), deye.DeviceAlertListRequest{DeviceSn: device.DeviceSn, DeviceType: device.DeviceType, StartTimestamp: now.AddDate(0, 0, -30).Unix(), EndTimestamp: now.Unix(), Page: 1, Size: 100})
	if err != nil {
		return deyeFail(c, err)
	}
	return c.JSON(reply)
}
