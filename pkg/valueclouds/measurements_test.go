package valueclouds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTypedMeasurementMethods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case loginPath:
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s","userId":73}}`))
		case "/ppe/api/auth/web/queryDeviceEnergyFlow":
			_, _ = w.Write([]byte(`{"code":0,"data":{"date":1795514002355,"status":0,"pv_status":[{"par":"pv_output_power","val":0,"unit":"W","status":0}],"bt_status":[{"par":"bt_battery_capacity","val":"70","unit":"%","status":1}]}}`))
		case "/ppe/api/auth/web/querySPDeviceLastData":
			_, _ = w.Write([]byte(`{"code":0,"data":{"gts":1795514002355,"pars":{"bc_":[{"id":"opaque_load_voltage","par":"Load voltage","val":"220","unit":"V"}],"bt_":[{"id":"bt_voltage_1","par":"Battery voltage","val":52.1,"unit":"V"}]}}}`))
		case "/ppe/api/auth/web/queryDevicePars":
			if r.URL.Query().Get("uid") != "73" {
				t.Error("UID must come from authenticated session")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":[{"date":"2026-11-24 15:53:22","name":"Total electricity purchased","par":"energy_total_from_grid","sortnum":18,"unit":"kWh","val":"21.94"}]}`))
		case "/dev/api/auth/valueCloud/dev/singleDeviceInfo":
			_, _ = w.Write([]byte(`{"code":0,"data":{"pn":"p","sn":"s","devcode":9,"devaddr":1,"deviceName":"Example","deviceSn":"physical-serial","firmwareVersion":"1.2.3","timezone":21600,"typeName":null,"collectorDTO":{"typeName":"WFBLE.DTU.Bar","firmwareVersion":"4.5.6"},"iotSysUserFeignDTO":{"account":"private-account"}}}`))
		case "/ppe/api/auth/web/queryDeviceChartsFields":
			if r.URL.Query().Get("uid") != "73" || r.URL.Query().Get("devcode") != "9" {
				t.Error("incorrect field configuration target")
			}
			_, _ = w.Write([]byte(`{"code":0,"data":[{"name":"Battery voltage","optional":"bt_voltage_1","order":28,"uint":"V","visable":false}]}`))
		default:
			t.Error("unexpected API route", r.URL.Path)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, Credentials{"operator", "hash"}, srv.Client())
	d := Device{PN: "p", SN: "s", Code: 9, Address: 1}
	ctx := context.Background()
	flow, err := c.DeviceEnergyFlow(ctx, d)
	if err != nil || flow.Timestamp != 1795514002355 || flow.Solar[0].Value != "0" || flow.Battery[0].Value != "70" {
		t.Fatalf("flow: %v %v", flow, err)
	}
	latest, err := c.DeviceLastData(ctx, d)
	if err != nil || latest.Groups["bc_"][0].Name != "Load voltage" || latest.Groups["bt_"][0].Value != "52.1" {
		t.Fatalf("latest: %v %v", latest, err)
	}
	pars, err := c.DeviceParameters(ctx, d)
	if err != nil || len(pars) != 1 || pars[0].Value != "21.94" || pars[0].Order != 18 {
		t.Fatalf("parameters: %v %v", pars, err)
	}
	info, err := c.DeviceInfo(ctx, d)
	if err != nil || info.Serial != "physical-serial" || info.Timezone != 21600 || info.Collector.TypeName != "WFBLE.DTU.Bar" {
		t.Fatalf("metadata: %v %v", info, err)
	}
	fields, err := c.DeviceChartFields(ctx, d)
	if err != nil || len(fields) != 1 || fields[0].Visible || fields[0].Parameter != "bt_voltage_1" || fields[0].Unit != "V" {
		t.Fatalf("fields: %v %v", fields, err)
	}
}
func TestValuePreservesGapsAndText(t *testing.T) {
	for _, test := range []struct{ raw, want string }{{`null`, ""}, {`0`, "0"}, {`"Single mode"`, "Single mode"}} {
		var v Value
		if err := json.Unmarshal([]byte(test.raw), &v); err != nil || string(v) != test.want {
			t.Fatalf("value %s: %v", test.raw, err)
		}
	}
}
