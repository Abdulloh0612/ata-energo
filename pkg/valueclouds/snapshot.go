package valueclouds

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
)

const MaxMeasurementAge = 10 * time.Minute
const MaxSourceSkew = time.Minute

// Snapshot is the shared boundary for CRM and background collection. Raw latest
// readings remain available even when they are too old for live monitoring.
type Snapshot struct {
	Latest      *LastData
	Payload     map[string]any
	CollectedAt time.Time
	Warnings    []string
}

func (c *Client) ReadSnapshot(ctx context.Context, d Device) (*Snapshot, error) {
	s := &Snapshot{Warnings: []string{}}
	latest, latestErr := c.DeviceLastData(ctx, d)
	if latestErr == nil {
		s.Latest = latest
	} else {
		s.Warnings = append(s.Warnings, "Не удалось получить последние измерения")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	flow, flowErr := c.EnergyFlow(ctx, d)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if flowErr != nil {
		s.Warnings = append(s.Warnings, "Не удалось получить потоки энергии")
	}
	if latestErr != nil && flowErr != nil {
		return nil, errors.Join(latestErr, flowErr)
	}
	s.CollectedAt = time.Now().UTC()
	if !d.Online() {
		s.Warnings = append(s.Warnings, "Устройство не подтверждает подключение к облаку")
		return s, nil
	}
	s.Payload = snapshotPayload(d.SN, s.Latest, flow, s.CollectedAt)
	if s.Payload == nil {
		s.Warnings = append(s.Warnings, "Нет свежих распознанных измерений")
	}
	return s, nil
}

// measurementTime accepts only finite, plausible Unix seconds/milliseconds.
func measurementTime(value any) (time.Time, bool) {
	n, ok := number(value)
	if !ok || n <= 0 {
		return time.Time{}, false
	}
	if n < 1e12 {
		n *= 1000
	}
	if n >= float64(math.MaxInt64) {
		return time.Time{}, false
	}
	return time.UnixMilli(int64(n)).UTC(), true
}

func FreshMeasurement(t, now time.Time) bool {
	return !t.IsZero() && !t.Before(now.Add(-MaxMeasurementAge)) && !t.After(now.Add(time.Minute))
}

func PayloadTime(payload map[string]any) time.Time {
	text, _ := payload["timestamp"].(string)
	t, _ := time.Parse(time.RFC3339, text)
	return t
}

func snapshotPayload(serial string, latest *LastData, flow map[string]any, now time.Time) map[string]any {
	flowTime, _ := measurementTime(flow["date"])
	var payload map[string]any
	if FreshMeasurement(flowTime, now) {
		payload, _ = BuildModemPayload(serial, flow)
	}
	var latestTime time.Time
	if latest != nil {
		latestTime, _ = measurementTime(float64(latest.Timestamp))
	}
	if !FreshMeasurement(latestTime, now) {
		return payload
	}
	if payload != nil {
		// Do not combine a new flow with old voltage/SOC from a different cycle.
		if delta := latestTime.Sub(flowTime); delta > MaxSourceSkew || delta < -MaxSourceSkew {
			return payload
		}
	} else {
		payload = map[string]any{"modem": serial, "timestamp": latestTime.Format(time.RFC3339)}
	}
	addLatestMeasurements(payload, latest)
	if len(payload) == 2 {
		return nil
	}
	return payload
}

type metricMapping struct{ group, section, field, unit string }

// Only source IDs with known semantics enter normalized telemetry. All other
// fields are retained in Snapshot.Latest, including model-specific registers.
var latestMetrics = map[string]metricMapping{
	"pv_output_power":      {"pv_", "pv", "power", "W"},
	"grid_active_power":    {"gd_", "grid", "power", "W"},
	"load_active_power":    {"bc_", "load", "power", "W"},
	"battery_active_power": {"bt_", "battery", "power", "W"},
	"gd_rating_voltage":    {"gd_", "grid", "voltage", "V"},
	"bt_voltage_1":         {"bt_", "battery", "voltage", "V"},
	"bt_battery_current":   {"bt_", "battery", "current", "A"},
	"bt_battery_capacity":  {"bt_", "battery", "soc", "%"},
}

func addLatestMeasurements(payload map[string]any, latest *LastData) {
	for group, points := range latest.Groups {
		for _, point := range points {
			m, ok := latestMetrics[point.ID]
			if !ok || m.group != group {
				continue
			}
			n, ok := number(string(point.Value))
			if !ok {
				continue
			}
			unit := strings.TrimSpace(point.Unit)
			if m.unit == "W" && strings.EqualFold(unit, "kW") {
				n *= 1000
			} else if !strings.EqualFold(unit, m.unit) {
				continue
			}
			if !validMetric(m.field, n) {
				continue
			}
			var part map[string]any
			if m.section == "pv" {
				parts, _ := payload["pv"].([]any)
				if len(parts) > 0 {
					part, _ = parts[0].(map[string]any)
				}
			} else {
				part, _ = payload[m.section].(map[string]any)
			}
			if part == nil {
				part = map[string]any{}
				if m.section == "pv" {
					payload[m.section] = []any{part}
				} else {
					payload[m.section] = part
				}
			}
			if _, exists := part[m.field]; !exists {
				part[m.field] = n
			}
		}
	}
}

func validMetric(field string, n float64) bool {
	return !math.IsNaN(n) && !math.IsInf(n, 0) &&
		(field != "soc" || n >= 0 && n <= 100) &&
		(field != "voltage" || n >= 0)
}
