package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ata-energo/pkg/valueclouds"
	"ata-energo/services"

	"github.com/gofiber/fiber/v2"
)

func TestValueCloudsWarningFields(t *testing.T) {
	alert := vcAlert(valueclouds.Device{PN: "p", SN: "s", Address: 1}, map[string]any{"desc": "Battery warning", "gts": float64(1700000000000), "code": "W1", "advice": "Check battery", "handle": true})
	if alert["alertName"] != "Battery warning" || alert["description"] != "Check battery" || alert["alertStartTime"] != int64(1700000000) || alert["status"] != 0 || alert["alertId"] == "" {
		t.Fatal(alert)
	}
}

func TestValueCloudsStationOmitsIncompleteSectionTotals(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	flows := []map[string]any{
		{"timestamp": now.Format(time.RFC3339), "grid": map[string]any{"power": 10.0}, "load": map[string]any{"power": 20.0}},
		{"timestamp": now.Format(time.RFC3339), "load": map[string]any{"power": 0.0}},
	}
	out := vcCompleteFlow(flows, 2)
	if _, exists := out["grid"]; exists {
		t.Fatal("partial grid total", out)
	}
	if out["load"].(map[string]any)["power"] != 20.0 {
		t.Fatal(out)
	}
	flows[1]["timestamp"] = now.Add(-2 * time.Minute).Format(time.RFC3339)
	if out = vcCompleteFlow(flows, 2); len(out) != 0 {
		t.Fatal("mixed acquisition times", out)
	}
}

func TestValueCloudsPreservesUnknownMeasurementGroups(t *testing.T) {
	out := vcReading(valueclouds.Device{SN: "s"}, &valueclouds.LastData{Groups: map[string][]valueclouds.Measurement{
		"extra_": {{ID: "opaque", Name: "Extra sensor", Value: "0", Unit: "V"}},
	}})
	points := out["dataList"].([]any)
	if len(points) != 1 || points[0].(fiber.Map)["source_group"] != "extra_" {
		t.Fatal(out)
	}
}

func TestValueCloudsAllTimeHistoryPreservesMissingMonths(t *testing.T) {
	year := time.Now().Year()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ppr/web/login/login" {
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
			return
		}
		if r.URL.Path != "/dcs/api/auth/web/ess/device/energyYearPerMonth" {
			t.Error(r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{
			map[string]any{"typeCode": 4, "vals": []any{
				map[string]any{"ts": fmt.Sprintf("%d-01", year), "val": 10.0},
				map[string]any{"ts": fmt.Sprintf("%d-02", year), "val": nil},
			}},
		}}})
	}))
	defer srv.Close()
	cl := valueclouds.NewClient(srv.URL, valueclouds.Credentials{Account: "a", PasswordSHA1: "h"}, srv.Client())
	s := services.NewValueCloudsService(nil, nil, nil, nil)
	points, err := vcHistory(context.Background(), s, cl, []valueclouds.Device{{PN: "p", SN: "s", Code: 1, Address: 1}}, "T", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0]["generationValue"] != nil {
		t.Fatal("partial year presented as total", points)
	}
}
func TestValueCloudsReadingNormalizesKilowatts(t *testing.T) {
	data := vcReading(valueclouds.Device{SN: "s", Status: 1}, map[string]any{"gts": float64(1700000000000), "pars": map[string]any{"gd_": []any{map[string]any{"id": "grid_active_power", "val": "1.5", "unit": "kW"}}}})
	point := data["dataList"].([]any)[0].(fiber.Map)
	if point["value"] != "1500" || point["unit"] != "W" || data["deviceState"] != 1 {
		t.Fatal(data)
	}
}

func TestValueCloudsReadingPreservesSourceGroupAndLabel(t *testing.T) {
	data := vcReading(valueclouds.Device{SN: "s", Status: 1}, &valueclouds.LastData{Timestamp: 1795514002355, Groups: map[string][]valueclouds.Measurement{
		"bc_": {{ID: "opaque_voltage", Name: "Load voltage", Value: "220", Unit: "V"}},
		"pv_": {{ID: "energy_total", Name: "Total PV power generation", Value: "2.19", Unit: "kWh"}},
	}})
	points := data["dataList"].([]any)
	for _, raw := range points {
		point := raw.(fiber.Map)
		if point["key"] == "opaque_voltage" && (point["group"] != "load" || point["label"] != "Load voltage") {
			t.Fatal("lost load source", point)
		}
		if point["key"] == "energy_total" && point["group"] != "pv" {
			t.Fatal("lost solar source", point)
		}
	}
}
