package api

import (
	"ata-energo/pkg/valueclouds"
	"github.com/gofiber/fiber/v2"
	"testing"
)

func TestValueCloudsWarningFields(t *testing.T) {
	alert := vcAlert(valueclouds.Device{PN: "p", SN: "s", Address: 1}, map[string]any{"desc": "Battery warning", "gts": float64(1700000000000), "code": "W1", "advice": "Check battery", "handle": true})
	if alert["alertName"] != "Battery warning" || alert["description"] != "Check battery" || alert["alertStartTime"] != int64(1700000000) || alert["status"] != 0 || alert["alertId"] == "" {
		t.Fatal(alert)
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
