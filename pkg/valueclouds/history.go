package valueclouds

import (
	"errors"
	"sort"
	"strconv"
	"time"
)

// HistoryPoints converts DCS series to the station dashboard's common units:
// daily power in W, monthly/yearly energy in kWh. Gaps remain nil.
func HistoryPoints(raw any, mode string, zone *time.Location) ([]map[string]any, error) {
	if mode != "D" && mode != "M" && mode != "Y" || zone == nil {
		return nil, errors.New("ValueClouds: некорректный период истории")
	}
	seen := map[string]bool{}
	data, ok := raw.(map[string]any)
	if !ok {
		return nil, errors.New("ValueClouds: некорректная история")
	}
	items, ok := data["items"].([]any)
	if !ok {
		return nil, errors.New("ValueClouds: нет рядов истории")
	}
	fields := []string{"purchaseValue", "gridValue", "chargeValue", "dischargeValue", "generationValue", "consumptionValue"}
	if mode == "D" {
		fields = []string{"purchasePower", "wirePower", "chargePower", "dischargePower", "generationPower", "consumptionPower"}
	}
	rows := map[string]map[string]any{}
	for _, item := range items {
		series, _ := item.(map[string]any)
		code, ok := number(series["typeCode"])
		if !ok || code < 0 || code > 5 || code != float64(int(code)) {
			continue
		}
		vals, _ := series["vals"].([]any)
		for _, val := range vals {
			point, _ := val.(map[string]any)
			ts, _ := point["ts"].(string)
			layout := "2006-01-02 15:04:05"
			if mode == "M" {
				layout = "2006-01-02"
			}
			if mode == "Y" {
				layout = "2006-01"
			}
			date, err := time.ParseInLocation(layout, ts, zone)
			if err != nil {
				return nil, errors.New("ValueClouds: некорректное время истории")
			}
			row := rows[ts]
			if row == nil {
				row = map[string]any{"timeStamp": strconv.FormatInt(date.Unix(), 10), "year": date.Year(), "month": nil, "day": nil}
				if mode != "T" {
					row["month"] = int(date.Month())
				}
				if mode == "M" || mode == "D" {
					row["day"] = date.Day()
				}
				rows[ts] = row
			}
			field := fields[int(code)]
			identity := ts + ":" + field
			if seen[identity] {
				return nil, errors.New("ValueClouds: повторяющееся измерение истории")
			}
			seen[identity] = true
			row[field] = nil
			if n, ok := number(point["val"]); ok {
				row[field] = n
			}
		}
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		row := rows[key]
		if mode == "D" {
			for _, pair := range []struct{ out, a, b string }{{"gridPower", "purchasePower", "wirePower"}, {"batteryPower", "chargePower", "dischargePower"}} {
				a, aok := row[pair.a].(float64)
				b, bok := row[pair.b].(float64)
				row[pair.out] = nil
				if aok && bok {
					row[pair.out] = a - b
				}
			}
		}
		out = append(out, row)
	}
	return out, nil
}
