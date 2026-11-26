package valueclouds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSnapshotFallsBackToLatestWithFreshTimestamp(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case loginPath:
			_, _ = w.Write([]byte(`{"code":200,"data":{"token":"t","secret":"s","userId":"user-demo"}}`))
		case "/ppe/api/auth/web/querySPDeviceLastData":
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{
				"gts": now.UnixMilli(), "pars": map[string]any{"pv_": []any{
					map[string]any{"id": "pv_output_power", "val": "1.25", "unit": "kW"},
				}},
			}})
		case "/ppe/api/auth/web/queryDeviceEnergyFlow":
			http.Error(w, "unavailable", 503)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	cl := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client())
	s, err := cl.ReadSnapshot(context.Background(), Device{PN: "p", SN: "s", Code: 1, Address: 1, Status: 1})
	if err != nil {
		t.Fatal(err)
	}
	if s.Latest == nil || s.Payload == nil || len(s.Warnings) != 1 {
		t.Fatalf("%+v", s)
	}
	if got := s.Payload["pv"].([]any)[0].(map[string]any)["power"]; got != 1250.0 {
		t.Fatal(got)
	}
	if PayloadTime(s.Payload) != now {
		t.Fatal(s.Payload)
	}
}

func TestSnapshotFreshnessAndSourceIsolation(t *testing.T) {
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	latest := func(at time.Time) *LastData {
		return &LastData{Timestamp: at.UnixMilli(), Groups: map[string][]Measurement{
			"bt_": {{ID: "bt_voltage_1", Value: "52.5", Unit: "V"}},
		}}
	}
	flow := func(at time.Time) map[string]any {
		return map[string]any{
			"date": float64(at.UnixMilli()), "pv_status": []any{map[string]any{"par": "pv_output_power", "val": "1000", "unit": "W"}},
		}
	}
	for _, tc := range []struct {
		name                     string
		latest, flow             time.Time
		wantPayload, wantBattery bool
	}{
		{"same cycle", now, now, true, true},
		{"different cycles", now.Add(-2 * time.Minute), now, true, false},
		{"stale latest", now.Add(-11 * time.Minute), now, true, false},
		{"both stale", now.Add(-11 * time.Minute), now.Add(-11 * time.Minute), false, false},
		{"future timestamps", now.Add(2 * time.Minute), now.Add(2 * time.Minute), false, false},
		{"latest fallback", now, now.Add(-11 * time.Minute), true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := snapshotPayload("s", latest(tc.latest), flow(tc.flow), now)
			if (p != nil) != tc.wantPayload {
				t.Fatal(p)
			}
			_, hasBattery := p["battery"]
			if hasBattery != tc.wantBattery {
				t.Fatal(p)
			}
		})
	}
}

func TestLatestMappingPreservesMissingValuesAndChecksUnits(t *testing.T) {
	now := time.Now().UTC()
	data := &LastData{Timestamp: now.UnixMilli(), Groups: map[string][]Measurement{
		"bt_": {
			{ID: "bt_battery_capacity", Value: "101", Unit: "%"},
			{ID: "bt_voltage_1", Value: "52", Unit: "A"},
			{ID: "bt_battery_current", Value: "-1.5", Unit: "A"},
		},
		"gd_": {{ID: "pv_output_power", Value: "99", Unit: "W"}},
		"pv_": {{ID: "pv_output_power", Value: "0", Unit: "W"}, {ID: "opaque_power", Value: "100", Unit: "W"}},
	}}
	p := snapshotPayload("s", data, nil, now)
	b := p["battery"].(map[string]any)
	if len(b) != 1 || b["current"] != -1.5 {
		t.Fatal(b)
	}
	if p["pv"].([]any)[0].(map[string]any)["power"] != 0.0 {
		t.Fatal(p)
	}
	if _, exists := p["grid"]; exists {
		t.Fatal("cross-group measurement mapped", p)
	}
	if _, exists := p["inverter"]; exists {
		t.Fatal("invented inverter state", p)
	}
}

func TestSnapshotRetainsOfflineReadingsWithoutPublishing(t *testing.T) {
	now := time.Now().UnixMilli()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == loginPath {
			_, _ = w.Write([]byte(`{"code":0,"data":{"token":"t","secret":"s"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"gts": now, "date": now, "pars": map[string]any{}}})
	}))
	defer srv.Close()
	s, err := NewClient(srv.URL, Credentials{"a", "h"}, srv.Client()).ReadSnapshot(context.Background(), Device{Status: 0})
	if err != nil || s.Latest == nil || s.Payload != nil {
		t.Fatalf("%+v %v", s, err)
	}
}

func TestLastDataAcceptsStringTimestamp(t *testing.T) {
	var d LastData
	if err := json.Unmarshal([]byte(`{"gts":"1795687200000","pars":{}}`), &d); err != nil || d.Timestamp != 1795687200000 {
		t.Fatalf("%+v %v", d, err)
	}
}
