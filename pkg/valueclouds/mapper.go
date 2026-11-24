package valueclouds

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

// BuildModemPayload maps only values actually supplied by the API. Missing values
// stay missing; a failed/empty response must never become a zero-filled reading.
func BuildModemPayload(serial string, flow map[string]any) (map[string]any, error) {
	msg := map[string]any{"modem": serial}
	timestamp, ok := number(flow["date"])
	if !ok || timestamp <= 0 {
		return nil, errors.New("ValueClouds: нет времени измерения")
	}
	if timestamp < 1e12 {
		timestamp *= 1000
	}
	msg["timestamp"] = time.UnixMilli(int64(timestamp)).UTC().Format(time.RFC3339)
	sections := map[string]string{"pv_status": "pv", "bt_status": "battery", "gd_status": "grid", "bc_status": "load", "ol_status": "generator"}
	fields := map[string]string{"pv_output_power": "power", "battery_active_power": "power", "bt_battery_capacity": "soc", "grid_active_power": "power", "load_active_power": "power", "oil_output_power": "power"}
	count := 0
	for group, section := range sections {
		items, _ := flow[group].([]any)
		part := map[string]any{}
		for _, item := range items {
			point, _ := item.(map[string]any)
			key, _ := point["par"].(string)
			field, known := fields[key]
			if !known {
				continue
			}
			val, ok := number(point["val"])
			if !ok {
				continue
			}
			unit, _ := point["unit"].(string)
			if field == "power" && strings.EqualFold(unit, "kW") {
				val *= 1000
			}
			if field == "power" && unit != "" && !strings.EqualFold(unit, "W") && !strings.EqualFold(unit, "kW") {
				continue
			}
			if field == "soc" && (val < 0 || val > 100) {
				continue
			}
			part[field] = val
			count++
		}
		if len(part) > 0 {
			if section == "pv" {
				msg[section] = []any{part}
			} else {
				msg[section] = part
			}
		}
	}
	if count > 0 {
		msg["inverter"] = map[string]any{"error": nil}
	}
	if count == 0 {
		return nil, errors.New("ValueClouds: нет распознанных измерений")
	}
	return msg, nil
}
func number(v any) (float64, bool) {
	var n float64
	var err error
	switch v := v.(type) {
	case float64:
		n = v
	case json.Number:
		n, err = v.Float64()
	case string:
		n, err = strconv.ParseFloat(v, 64)
	default:
		return 0, false
	}
	return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
}

// EnrichPayload adds stable voltage/current IDs from querySPDeviceLastData.
// Model-specific register IDs remain available in the raw dashboard readings.
func EnrichPayload(payload map[string]any, latest any) {
	data, _ := latest.(map[string]any)
	groups, _ := data["pars"].(map[string]any)
	fields := map[string]struct{ section, field string }{
		"gd_rating_voltage": {"grid", "voltage"}, "bt_voltage_1": {"battery", "voltage"},
		"bt_battery_current": {"battery", "current"}, "bt_battery_capacity": {"battery", "soc"},
	}
	for _, group := range []string{"gd_", "bt_"} {
		items, _ := groups[group].([]any)
		for _, item := range items {
			point, _ := item.(map[string]any)
			id, _ := point["id"].(string)
			mapping, ok := fields[id]
			if !ok {
				continue
			}
			val, ok := number(point["val"])
			if !ok {
				continue
			}
			part, _ := payload[mapping.section].(map[string]any)
			if part == nil {
				part = map[string]any{}
				payload[mapping.section] = part
			}
			if _, exists := part[mapping.field]; !exists {
				part[mapping.field] = val
			}
		}
	}
}
