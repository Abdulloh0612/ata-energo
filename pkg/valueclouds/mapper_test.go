package valueclouds

import (
	"encoding/json"
	"testing"
	"time"
)

func decoded(t *testing.T, s string) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal([]byte(s), &data); err != nil {
		t.Fatal(err)
	}
	return data
}
func TestFlowUnitsAndMissingValues(t *testing.T) {
	flow := decoded(t, `{"date":1700000000000,"pv_status":[{"par":"pv_output_power","val":"1.25","unit":"kW"}],"bt_status":[{"par":"bt_battery_capacity","val":"71","unit":"%"}],"gd_status":[{"par":"grid_active_power","val":"0","unit":"W"}],"bc_status":[{"par":"load_active_power","val":"--","unit":"W"}]}`)
	msg, err := BuildModemPayload("test", flow)
	if err != nil {
		t.Fatal(err)
	}
	if msg["pv"].([]any)[0].(map[string]any)["power"] != float64(1250) {
		t.Fatal(msg)
	}
	if msg["grid"].(map[string]any)["power"] != float64(0) {
		t.Fatal("zero lost")
	}
	if _, ok := msg["load"]; ok {
		t.Fatal("missing data became zero")
	}
	if _, ok := msg["battery"].(map[string]any)["power"]; ok {
		t.Fatal("invented battery power")
	}
	if msg["battery"].(map[string]any)["soc"] != float64(71) {
		t.Fatal(msg)
	}
}
func TestFlowRejectsEmptyOrUndatedReadings(t *testing.T) {
	for _, raw := range []string{`{}`, `{"date":1700000000000}`, `{"date":0,"pv_status":[{"par":"pv_output_power","val":"1","unit":"kW"}]}`} {
		if _, err := BuildModemPayload("test", decoded(t, raw)); err == nil {
			t.Fatal("accepted unusable telemetry")
		}
	}
}

func TestHistoryRejectsDuplicateSamples(t *testing.T) {
	raw := decoded(t, `{"items":[{"typeCode":4,"vals":[{"ts":"2026-01","val":1},{"ts":"2026-01","val":2}]}]}`)
	if _, err := HistoryPoints(raw, "Y", time.UTC); err == nil {
		t.Fatal("duplicate sample silently overwritten")
	}
}

func TestFlowRejectsMetricsInWrongSourceGroup(t *testing.T) {
	raw := decoded(t, `{"date":1700000000000,"pv_status":[{"par":"bt_battery_capacity","val":"50","unit":"%"}]}`)
	if _, err := BuildModemPayload("s", raw); err == nil {
		t.Fatal("SOC entered PV measurements")
	}
}
func TestHistoryGapsUnitsAndTimezone(t *testing.T) {
	raw := decoded(t, `{"items":[{"typeCode":0,"vals":[{"ts":"2026-11-24 00:00:00","val":"1000"}]},{"typeCode":1,"vals":[{"ts":"2026-11-24 00:00:00","val":null}]},{"typeCode":2,"vals":[{"ts":"2026-11-24 00:00:00","val":"0"}]},{"typeCode":3,"vals":[{"ts":"2026-11-24 00:00:00","val":"74"}]}]}`)
	zone, _ := time.LoadLocation("Asia/Bishkek")
	rows, err := HistoryPoints(raw, "D", zone)
	if err != nil {
		t.Fatal(err)
	}
	row := rows[0]
	if row["gridPower"] != nil {
		t.Fatal("missing sale became zero")
	}
	if row["batteryPower"] != float64(-74) {
		t.Fatal("battery discharge sign lost")
	}
	if row["purchasePower"] != float64(1000) {
		t.Fatal("watts changed")
	}
	if row["timeStamp"] != "1795456800" {
		t.Fatalf("incorrect timezone: %v", row["timeStamp"])
	}
	monthly := decoded(t, `{"items":[{"typeCode":4,"vals":[{"ts":"2026-11-22","val":"7.12"}]}]}`)
	rows, err = HistoryPoints(monthly, "M", zone)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0]["generationValue"] != 7.12 || rows[0]["day"] != 22 {
		t.Fatal(rows)
	}
}
