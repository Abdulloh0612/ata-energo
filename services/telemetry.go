package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/pkg/hub"
	"ata-energo/types"

	"go.uber.org/zap"
)

// Главная логика обработки данных. Используется MQTT-воркером, агентом, Deye Cloud и демо-режимом.
//
//	ProcessModemMessage(serial, data) - сообщение модема инвертора (ВСЕ данные станции сразу)
//	ProcessReading(data)              - сообщение одного устройства (старый формат с device_id)
//	CheckOfflineDevices()             - нет данных > DEVICE_OFFLINE_TIMEOUT -> offline + Alert
//
// Формат сообщения модема (топик inverter/<SN модема>/data):
//
//	{
//	  "modem": "2710000001",                     (можно не писать — возьмём из топика)
//	  "inverter":  {"power": 2875, "voltage": 230, "current": 12.5, "temperature": 41, "error": null},
//	  "pv":        [{"power": 1500, "voltage": 360, "current": 4.2}, {"power": 1400, ...}],
//	  "battery":   {"power": -800, "voltage": 51.2, "current": -15.6, "soc": 80, "temperature": 25},
//	  "grid":      {"power": 0, "voltage": 228, "current": 0},
//	  "generator": {"power": 0, "voltage": 0, "current": 0},
//	  "load":      {"power": 2875, "voltage": 230, "current": 12.5}
//	}
//
// Знаки: батарея + заряд / - разряд; сеть + покупка / - продажа.

// GridMinVoltage — если напряжение сети меньше, считаем, что сеть отключена.
const GridMinVoltage = 50.0

type TelemetryService struct {
	modems    repositories.ModemRepository
	devices   repositories.DeviceRepository
	telemetry repositories.TelemetryRepository
	alerts    repositories.AlertRepository
	modemSvc  *ModemService
	hub       *hub.Hub
	log       *zap.Logger

	offlineTimeout      time.Duration
	cloudOfflineTimeout time.Duration
}

func NewTelemetryService(
	modems repositories.ModemRepository,
	devices repositories.DeviceRepository,
	telemetry repositories.TelemetryRepository,
	alerts repositories.AlertRepository,
	modemSvc *ModemService,
	h *hub.Hub,
	log *zap.Logger,
	offlineTimeout, cloudOfflineTimeout time.Duration,
) *TelemetryService {
	if cloudOfflineTimeout < offlineTimeout {
		cloudOfflineTimeout = offlineTimeout
	}
	return &TelemetryService{
		modems: modems, devices: devices, telemetry: telemetry, alerts: alerts, modemSvc: modemSvc,
		hub: h, log: log, offlineTimeout: offlineTimeout, cloudOfflineTimeout: cloudOfflineTimeout,
	}
}

func (s *TelemetryService) Hub() *hub.Hub { return s.hub }

// ─── helpers ─────────────────────────────────────────────────────────────────

// ToNumber превращает значение в число (округлённое до 0.01). Если нельзя — nil.
func ToNumber(v any) *float64 {
	var f float64
	switch x := v.(type) {
	case nil:
		return nil
	case float64:
		f = x
	case float32:
		f = float64(x)
	case int:
		f = float64(x)
	case int64:
		f = float64(x)
	case json.Number:
		p, err := x.Float64()
		if err != nil {
			return nil
		}
		f = p
	case string:
		x = strings.TrimSpace(x)
		if x == "" {
			return nil
		}
		p, err := strconv.ParseFloat(x, 64)
		if err != nil {
			return nil
		}
		f = p
	case bool:
		if x {
			f = 1
		}
	default:
		return nil
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	f = math.Round(f*100) / 100
	return &f
}

// AsString — значение JSON как строка (как str() в Python): nil -> "", 2712345678 -> "2712345678"
// (а не "2.712345678e+09", как дал бы fmt.Sprint для float64).
func AsString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e18 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case json.Number:
		return x.String()
	case bool:
		if x {
			return "True"
		}
		return "False"
	}
	return fmt.Sprint(v)
}

// truthy — как в Python: None, "", 0, False, пустые списки -> false.
func truthy(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return x != ""
	case bool:
		return x
	case float64:
		return x != 0
	case int:
		return x != 0
	case json.Number:
		return x.String() != "0"
	case []any:
		return len(x) > 0
	case map[string]any:
		return len(x) > 0
	}
	return true
}

func errorText(v any) *string {
	if !truthy(v) {
		return nil
	}
	var s string
	if str, ok := v.(string); ok {
		s = str
	} else {
		s = AsString(v)
	}
	if r := []rune(s); len(r) > 255 {
		s = string(r[:255])
	}
	return &s
}

func ptr(f float64) *float64 { return &f }

// valuesFrom достаёт числа из раздела сообщения. Для батареи "soc" = заряд в %.
func valuesFrom(item map[string]any, deviceType string) types.Values {
	v := types.Values{
		Current:        ToNumber(item["current"]),
		Voltage:        ToNumber(item["voltage"]),
		Power:          ToNumber(item["power"]),
		BatteryPercent: ToNumber(item["battery_percent"]),
		Temperature:    ToNumber(item["temperature"]),
	}
	if deviceType == types.DeviceTypeBattery && v.BatteryPercent == nil {
		v.BatteryPercent = ToNumber(item["soc"])
	}
	return v
}

func lastFloat(m map[string]any, key string) *float64 {
	if m == nil {
		return nil
	}
	return ToNumber(m[key])
}

func lastString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// ─── alerts / save ───────────────────────────────────────────────────────────

// CreateAlert создаёт аварию в базе и сразу отправляет её в WebSocket.
func (s *TelemetryService) CreateAlert(ctx context.Context, d *types.Device, kind, text string) (*types.Alert, error) {
	a, err := s.alerts.Create(ctx, d.ID, kind, text)
	if err != nil || a == nil {
		return a, err
	}
	s.hub.Broadcast(d.ClientID, map[string]any{
		"type": "alert",
		"alert": map[string]any{
			"id":          a.ID,
			"kind":        a.Kind,
			"text":        a.Text,
			"created_at":  a.CreatedAt,
			"is_read":     false,
			"device":      d.Info(),
			"client_name": d.ClientName,
		},
	})
	return a, nil
}

// SaveValues сохраняет показания одного устройства, обновляет его и рассылает в WebSocket.
func (s *TelemetryService) SaveValues(ctx context.Context, d *types.Device, v types.Values, errText *string, now time.Time) error {
	if err := s.telemetry.Insert(ctx, d.ID, now, v, errText); err != nil {
		return err
	}

	old := d.LastData
	d.Status = types.StatusOnline
	d.LastSeen = &now
	var errVal any
	if errText != nil {
		errVal = *errText
	}
	d.LastData = map[string]any{
		"current":         v.Current,
		"voltage":         v.Voltage,
		"power":           v.Power,
		"battery_percent": v.BatteryPercent,
		"temperature":     v.Temperature,
		"error":           errVal,
		"timestamp":       now.Format(time.RFC3339Nano),
	}
	if err := s.devices.SaveLast(ctx, d); err != nil {
		return err
	}

	// Новая ошибка -> авария
	if errText != nil && *errText != lastString(old, "error") {
		text := fmt.Sprintf("%s %s: %s", types.DeviceTypeDisplay[d.DeviceType], d.DeviceID, *errText)
		if _, err := s.CreateAlert(ctx, d, types.AlertKindError, text); err != nil {
			s.log.Error("create alert", zap.Error(err))
		}
	}

	// Пропала сеть (было напряжение, стало ~0) -> авария
	if d.DeviceType == types.DeviceTypeGrid {
		oldV, newV := lastFloat(old, "voltage"), v.Voltage
		if oldV != nil && *oldV >= GridMinVoltage && newV != nil && *newV < GridMinVoltage {
			text := fmt.Sprintf("Сеть отключена у клиента «%s»", d.ClientName)
			if _, err := s.CreateAlert(ctx, d, types.AlertKindError, text); err != nil {
				s.log.Error("create alert", zap.Error(err))
			}
		}
	}

	s.hub.Broadcast(d.ClientID, map[string]any{"type": "telemetry", "device": d.Info(), "data": d.LastData})
	return nil
}

// ─── processing ──────────────────────────────────────────────────────────────

// ProcessModemMessage обрабатывает сообщение модема со всеми данными станции.
//
// Разделы, которых нет в сообщении (например, генератор выключен и модем его не прислал),
// записываются как 0 — то есть "отключено". Возвращает модем или nil, если модем не зарегистрирован.
func (s *TelemetryService) ProcessModemMessage(ctx context.Context, serial string, data map[string]any) (*types.Modem, error) {
	serial = strings.TrimSpace(serial)
	m, err := s.modems.GetBySerial(ctx, serial)
	if err != nil {
		return nil, err
	}
	if m == nil {
		s.log.Warn("Неизвестный модем — зарегистрируйте его у клиента (поле «SN модема»)", zap.String("serial", serial))
		return nil, nil
	}

	now := time.Now().UTC()
	wasOffline := m.Status != types.StatusOnline
	if err := s.modems.SetOnline(ctx, m.ID, now); err != nil {
		return nil, err
	}
	m.Status = types.StatusOnline
	m.LastSeen = &now

	list, err := s.devices.ListByModem(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	devices := make(map[string]*types.Device, len(list))
	var order []string
	for i := range list {
		devices[list[i].DeviceID] = &list[i]
		order = append(order, list[i].DeviceID)
	}
	updated := map[string]bool{}

	for _, sec := range Sections {
		raw, ok := data[sec.Name]
		if !ok || raw == nil {
			continue
		}
		var items []any
		if arr, isList := raw.([]any); isList {
			items = arr
		} else {
			items = []any{raw}
		}
		for i, it := range items {
			item, isMap := it.(map[string]any)
			if !isMap {
				continue
			}
			number := i + 1
			code := DeviceCode(m.Serial, sec.Type, number)
			d := devices[code]
			if d == nil {
				// модем прислал то, чего ещё нет (вторая строка панелей, генератор) — создаём
				if sec.Type == types.DeviceTypeGenerator && !m.HasGenerator {
					m.HasGenerator = true
					if err := s.modems.SetHasGenerator(ctx, m.ID, true); err != nil {
						return nil, err
					}
				}
				d, err = s.modemSvc.EnsureDevice(ctx, m, sec.Type, number)
				if err != nil {
					return nil, err
				}
				devices[code] = d
				order = append(order, code)
			}
			if err := s.SaveValues(ctx, d, valuesFrom(item, sec.Type), errorText(item["error"]), now); err != nil {
				return nil, err
			}
			updated[code] = true
		}
	}

	// Чего нет в сообщении — значит выключено (0 Вт, 0 В)
	for _, code := range order {
		if updated[code] {
			continue
		}
		d := devices[code]
		v := types.Values{Current: ptr(0), Voltage: ptr(0), Power: ptr(0), BatteryPercent: ptr(0), Temperature: ptr(0)}
		if d.DeviceType == types.DeviceTypeBattery {
			v.BatteryPercent = lastFloat(d.LastData, "battery_percent")
		}
		if err := s.SaveValues(ctx, d, v, nil, now); err != nil {
			return nil, err
		}
	}

	if wasOffline {
		s.log.Info("Модем снова в сети", zap.String("serial", serial))
	}
	s.hub.Broadcast(m.ClientID, map[string]any{"type": "modem", "modem": m.Info()})
	return m, nil
}

// ProcessReading обрабатывает сообщение ОДНОГО устройства (формат с "device_id").
//
//	{"device_id": "INV001", "current": 12.5, "voltage": 230, "power": 2875, ...}
func (s *TelemetryService) ProcessReading(ctx context.Context, data map[string]any) (*types.Device, error) {
	deviceID := strings.TrimSpace(AsString(data["device_id"]))
	if deviceID == "" {
		s.log.Warn("Сообщение без device_id", zap.Any("data", data))
		return nil, nil
	}
	d, err := s.devices.GetByDeviceID(ctx, deviceID)
	if err != nil {
		return nil, err
	}
	if d == nil {
		s.log.Warn("Неизвестное устройство", zap.String("device_id", deviceID))
		return nil, nil
	}
	return d, s.SaveValues(ctx, d, valuesFrom(data, d.DeviceType), errorText(data["error"]), time.Now().UTC())
}

func valueOr(v any, def any) any {
	if v == nil {
		return def
	}
	return v
}

// ProcessMessage определяет формат сообщения и вызывает нужную функцию.
//
//	есть "device_id" (и нет разделов)    -> одно устройство
//	есть разделы inverter/pv/battery/... -> модем (SN из "modem"/"serial"/"sn" или из топика)
func (s *TelemetryService) ProcessMessage(ctx context.Context, data map[string]any, topic string) error {
	if data == nil {
		return nil
	}
	hasSections := false
	for _, sec := range Sections {
		if _, ok := data[sec.Name]; ok {
			hasSections = true
			break
		}
	}
	if truthy(data["device_id"]) && !hasSections {
		_, err := s.ProcessReading(ctx, data)
		return err
	}
	var serial string
	for _, key := range []string{"modem", "serial", "sn"} {
		if truthy(data[key]) {
			serial = AsString(data[key])
			break
		}
	}
	if serial == "" {
		parts := strings.Split(topic, "/")
		if len(parts) >= 2 {
			serial = parts[1]
		}
	}
	_, err := s.ProcessModemMessage(ctx, serial, data)
	return err
}

// CheckOfflineDevices ставит offline тому, от кого нет данных дольше таймаута.
//
// Для модема создаётся ОДНА авария "нет связи с модемом" (а не по аварии на каждое устройство).
// Возвращает количество устройств, которые стали offline.
func (s *TelemetryService) CheckOfflineDevices(ctx context.Context) (int, error) {
	now := time.Now().UTC()
	limit := now.Add(-s.offlineTimeout)
	cloudLimit := now.Add(-s.cloudOfflineTimeout)
	timeout := int(s.offlineTimeout.Seconds())

	// 1) Модемы
	stale, err := s.modems.ListStaleOnline(ctx, limit, cloudLimit)
	if err != nil {
		return 0, err
	}
	for i := range stale {
		m := &stale[i]
		changed, err := s.modems.MarkOfflineIfOnline(ctx, m.ID)
		if err != nil {
			return 0, err
		}
		if !changed {
			continue
		}
		m.Status = types.StatusOffline
		mainDev, err := s.devices.FirstByModem(ctx, m.ID)
		if err != nil {
			return 0, err
		}
		if mainDev != nil {
			t := timeout
			if m.Protocol() == types.ProtocolDeyeCloud {
				t = int(s.cloudOfflineTimeout.Seconds())
			}
			text := fmt.Sprintf("Нет связи с модемом %s («%s») более %d сек.", m.Serial, m.ClientName, t)
			if _, err := s.CreateAlert(ctx, mainDev, types.AlertKindOffline, text); err != nil {
				s.log.Error("create alert", zap.Error(err))
			}
		}
		s.hub.Broadcast(m.ClientID, map[string]any{"type": "modem", "modem": m.Info()})
	}

	// 2) Устройства
	count := 0
	devs, err := s.devices.ListStaleOnline(ctx, limit, cloudLimit)
	if err != nil {
		return 0, err
	}
	for i := range devs {
		d := &devs[i]
		changed, err := s.devices.MarkOfflineIfOnline(ctx, d.ID)
		if err != nil {
			return count, err
		}
		if !changed {
			continue
		}
		count++
		d.Status = types.StatusOffline
		if d.ModemID == nil { // у устройств модема авария уже создана выше
			text := fmt.Sprintf("Нет связи с устройством %s (%s) более %d сек.",
				d.DeviceID, types.DeviceTypeDisplay[d.DeviceType], timeout)
			if _, err := s.CreateAlert(ctx, d, types.AlertKindOffline, text); err != nil {
				s.log.Error("create alert", zap.Error(err))
			}
		}
		s.hub.Broadcast(d.ClientID, map[string]any{"type": "status", "device": d.Info()})
	}
	return count, nil
}
