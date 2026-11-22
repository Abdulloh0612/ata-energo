package deye

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeDeye — сервер, похожий на Deye Cloud OpenAPI.
func fakeDeye(t *testing.T, tokenCalls *int32, expireFirst bool) *httptest.Server {
	var latestCalls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1.0/account/token":
			atomic.AddInt32(tokenCalls, 1)
			if r.URL.Query().Get("appId") != "APP" {
				_, _ = w.Write([]byte(`{"code":"2101021","msg":"auth invalid appId","success":false,"requestId":"x"}`))
				return
			}
			var body TokenRequest
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.AppSecret != "SECRET" || body.Email != "a@b.c" || body.Password != HashPassword("pass") {
				t.Errorf("token body: %+v", body)
			}
			_, _ = w.Write([]byte(`{"code":"1000000","msg":"success","success":true,"requestId":"r",
				"accessToken":"tok123","tokenType":"bearer","expiresIn":"5183999","uid":3}`))
		case "/v1.0/device/latest":
			if r.Header.Get("Authorization") != "Bearer tok123" {
				t.Errorf("auth header = %q", r.Header.Get("Authorization"))
			}
			if expireFirst && atomic.AddInt32(&latestCalls, 1) == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"code":"2101019","msg":"token expired","success":false,"requestId":"e"}`))
				return
			}
			_, _ = w.Write([]byte(`{"code":1000000,"msg":"success","success":true,"requestId":"r","deviceDataList":[
				{"deviceSn":"SN1","deviceType":"INVERTER","deviceState":1,"collectionTime":1700000000,"dataList":[
					{"key":"SOC","value":"81","unit":"%"},
					{"key":"Battery Power","value":"-1.5","unit":"kW"},
					{"key":"Battery Voltage","value":"52.1","unit":"V"},
					{"key":"PV1 Power","value":1200,"unit":"W"},
					{"key":"PV1 Voltage","value":"350.5","unit":"V"},
					{"key":"PV2 Power","value":"800","unit":"W"},
					{"key":"Total Grid Power","value":"-300","unit":"W"},
					{"key":"Grid Voltage L1","value":"229.4","unit":"V"},
					{"key":"Total Consumption Power","value":"2.3","unit":"kW"},
					{"key":"DC Temperature","value":"41.2","unit":"℃"},
					{"key":"Running Status","value":"Normal","unit":""}
				]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestClient(url string) *Client {
	return NewClient(url, Credentials{AppID: "APP", AppSecret: "SECRET", Email: "a@b.c", PasswordSHA256: HashPassword("pass")}, nil)
}

func TestTokenCachedAndLatest(t *testing.T) {
	var calls int32
	srv := fakeDeye(t, &calls, false)
	defer srv.Close()
	c := newTestClient(srv.URL)
	for i := 0; i < 3; i++ {
		resp, err := c.DeviceLatest(context.Background(), DeviceLatestRequest{DeviceList: []string{"SN1"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(resp.DeviceDataList) != 1 || resp.DeviceDataList[0].DeviceSn != "SN1" {
			t.Fatalf("unexpected: %+v", resp)
		}
	}
	if calls != 1 {
		t.Fatalf("token requested %d times, want 1 (cache)", calls)
	}
}

func TestRetryOnExpiredToken(t *testing.T) {
	var calls int32
	srv := fakeDeye(t, &calls, true)
	defer srv.Close()
	c := newTestClient(srv.URL)
	if _, err := c.DeviceLatest(context.Background(), DeviceLatestRequest{DeviceList: []string{"SN1"}}); err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("token requested %d times, want 2 (refresh after expiry)", calls)
	}
}

func TestAPIError(t *testing.T) {
	var calls int32
	srv := fakeDeye(t, &calls, false)
	defer srv.Close()
	c := NewClient(srv.URL, Credentials{AppID: "BAD", AppSecret: "x", Email: "e", PasswordSHA256: "p"}, nil)
	_, err := c.AccountInfo(context.Background())
	var ae *APIError
	if err == nil || !strings.Contains(err.Error(), "2101021") {
		t.Fatalf("want APIError 2101021, got %v", err)
	}
	_ = ae
}

func TestBuildModemPayload(t *testing.T) {
	var calls int32
	srv := fakeDeye(t, &calls, false)
	defer srv.Close()
	resp, err := newTestClient(srv.URL).DeviceLatest(context.Background(), DeviceLatestRequest{DeviceList: []string{"SN1"}})
	if err != nil {
		t.Fatal(err)
	}
	msg := BuildModemPayload("SN1", &resp.DeviceDataList[0], nil, MapOptions{})

	pv := msg["pv"].([]any)
	if len(pv) != 2 || pv[0].(map[string]any)["power"] != 1200.0 || pv[0].(map[string]any)["voltage"] != 350.5 {
		t.Fatalf("pv = %v", pv)
	}
	bat := msg["battery"].(map[string]any)
	// Deye: -1.5 kW (заряд) -> наш формат: +1500 Вт (заряд)
	if bat["power"] != 1500.0 || bat["soc"] != 81.0 || bat["voltage"] != 52.1 {
		t.Fatalf("battery = %v", bat)
	}
	grid := msg["grid"].(map[string]any)
	if grid["power"] != -300.0 || grid["voltage"] != 229.4 {
		t.Fatalf("grid = %v", grid)
	}
	load := msg["load"].(map[string]any)
	if load["power"] != 2300.0 {
		t.Fatalf("load = %v", load)
	}
	inv := msg["inverter"].(map[string]any)
	if inv["temperature"] != 41.2 || inv["error"] != nil {
		t.Fatalf("inverter = %v", inv)
	}
	if _, ok := msg["generator"]; ok {
		t.Fatalf("generator must be absent: %v", msg["generator"])
	}

	// с инверсией батареи
	msg = BuildModemPayload("SN1", &resp.DeviceDataList[0], nil, MapOptions{InvertBattery: true})
	if msg["battery"].(map[string]any)["power"] != -1500.0 {
		t.Fatalf("invert battery failed: %v", msg["battery"])
	}
}

func TestBuildFromStationOnly(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	st := &StationLatestResponse{
		GenerationPower: f(4000), ConsumptionPower: f(1500), ChargePower: f(2000), DischargePower: f(0),
		PurchasePower: f(0), WirePower: f(500), BatterySOC: f(64),
	}
	msg := BuildModemPayload("SN2", nil, st, MapOptions{})
	if msg["pv"].([]any)[0].(map[string]any)["power"] != 4000.0 {
		t.Fatalf("pv = %v", msg["pv"])
	}
	bat := msg["battery"].(map[string]any)
	if bat["power"] != 2000.0 || bat["soc"] != 64.0 {
		t.Fatalf("battery = %v", bat)
	}
	if msg["grid"].(map[string]any)["power"] != -500.0 {
		t.Fatalf("grid = %v", msg["grid"])
	}
	if msg["load"].(map[string]any)["power"] != 1500.0 {
		t.Fatalf("load = %v", msg["load"])
	}
}

func TestFlexTypes(t *testing.T) {
	var v struct {
		C Code       `json:"c"`
		S FlexString `json:"s"`
		I FlexInt    `json:"i"`
	}
	if err := json.Unmarshal([]byte(`{"c":1000000,"s":223.4,"i":"5183999"}`), &v); err != nil {
		t.Fatal(err)
	}
	if v.C != CodeSuccess || v.S != "223.4" || v.I != 5183999 {
		t.Fatalf("%+v", v)
	}
	if NormKey("Grid Voltage L1-L2") != "gridvoltagel1l2" {
		t.Fatal(NormKey("Grid Voltage L1-L2"))
	}
}

func TestDeviceHistoryRawBatchedJoinsSparseTimestamps(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1.0/account/token" {
			_, _ = w.Write([]byte(`{"code":"1000000","success":true,"accessToken":"tok","expiresIn":3600}`))
			return
		}
		var req DeviceHistoryRawRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
		}
		if len(req.MeasurePoints) > 5 || req.DeviceSn != "SN1" || req.StartTimestamp != 100 {
			t.Errorf("invalid batch: %+v", req)
		}
		calls++
		if calls == 1 {
			_, _ = w.Write([]byte(`{"code":"1000000","success":true,"deviceSn":"SN1","dataList":[{"time":"200","itemList":[{"key":"PV","value":"0"}]},{"time":"100","itemList":[{"key":"PV","value":"12"}]}]}`))
		} else {
			_, _ = w.Write([]byte(`{"code":"1000000","success":true,"dataList":[{"time":"200","itemList":[{"key":"SOC","value":"97"}]},{"time":"300","itemList":[{"key":"SOC","value":"98"}]}]}`))
		}
	}))
	defer srv.Close()
	got, err := newTestClient(srv.URL).DeviceHistoryRawBatched(context.Background(), DeviceHistoryRawRequest{DeviceSn: "SN1", StartTimestamp: 100, EndTimestamp: 300, MeasurePoints: []string{"PV", "V", "A", "Grid", "Load", "SOC"}})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || got.DeviceSn != "SN1" || len(got.DataList) != 3 {
		t.Fatalf("unexpected result: calls=%d reply=%+v", calls, got)
	}
	if got.DataList[0].Time != "100" || len(got.DataList[0].ItemList) != 1 || got.DataList[1].Time != "200" || len(got.DataList[1].ItemList) != 2 || got.DataList[2].Time != "300" || len(got.DataList[2].ItemList) != 1 {
		t.Fatalf("timestamps not correctly joined: %+v", got.DataList)
	}
}
