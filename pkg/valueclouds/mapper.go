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
	timestamp, ok := measurementTime(flow["date"])
	if !ok {
		return nil, errors.New("ValueClouds: нет времени измерения")
	}
	msg["timestamp"] = timestamp.Format(time.RFC3339)
	sections := map[string]string{"pv_status": "pv", "bt_status": "battery", "gd_status": "grid", "bc_status": "load", "ol_status": "generator"}
	fields := map[string]map[string]string{
		"pv_status": {"pv_output_power": "power"},
		"bt_status": {"battery_active_power": "power", "bt_battery_capacity": "soc"},
		"gd_status": {"grid_active_power": "power"},
		"bc_status": {"load_active_power": "power"},
		"ol_status": {"oil_output_power": "power"},
	}
	count := 0
	for group, section := range sections {
		items, _ := flow[group].([]any)
		part := map[string]any{}
		for _, item := range items {
			point, _ := item.(map[string]any)
			key, _ := point["par"].(string)
			field, known := fields[group][key]
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
			if !validMetric(field, val) || field == "soc" && unit != "%" {
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

// EnrichPayload only merges readings from the same acquisition window.
// New callers should use ReadSnapshot so freshness policy is shared.
func EnrichPayload(payload map[string]any, latest any) {
	if payload == nil {
		return
	}
	encoded, err := json.Marshal(latest)
	if err != nil {
		return
	}
	var data LastData
	if json.Unmarshal(encoded, &data) != nil {
		return
	}
	measured, ok := measurementTime(float64(data.Timestamp))
	base := PayloadTime(payload)
	if !ok || base.IsZero() || measured.Sub(base) > MaxSourceSkew || base.Sub(measured) > MaxSourceSkew {
		return
	}
	addLatestMeasurements(payload, &data)
}
