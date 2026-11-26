package services

import (
	"context"
	"testing"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/pkg/hub"
	"ata-energo/types"

	"go.uber.org/zap"
)

type vcTelemetryModems struct {
	repositories.ModemRepository
	modem  *types.Modem
	online []time.Time
}

func (r *vcTelemetryModems) GetBySerial(context.Context, string) (*types.Modem, error) {
	return r.modem, nil
}
func (r *vcTelemetryModems) SetOnline(_ context.Context, _ int64, at time.Time) error {
	r.online = append(r.online, at)
	return nil
}

type vcTelemetryDevices struct {
	repositories.DeviceRepository
	devices []types.Device
	saved   []*types.Device
}

func (r *vcTelemetryDevices) ListByModem(context.Context, int64) ([]types.Device, error) {
	return r.devices, nil
}
func (r *vcTelemetryDevices) SaveLast(_ context.Context, d *types.Device) error {
	r.saved = append(r.saved, d)
	return nil
}

type vcTelemetryRows struct {
	repositories.TelemetryRepository
	times  []time.Time
	values []types.Values
}

func (r *vcTelemetryRows) Insert(_ context.Context, _ int64, at time.Time, v types.Values, _ *string) error {
	r.times = append(r.times, at)
	r.values = append(r.values, v)
	return nil
}

func TestValueCloudsIngestionPreservesMeasurementTimeAndMissingSections(t *testing.T) {
	at := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	modems := &vcTelemetryModems{modem: &types.Modem{ID: 1, Serial: "vc", Status: types.StatusOnline, Connection: map[string]any{"protocol": types.ProtocolValueClouds}}}
	devices := &vcTelemetryDevices{devices: []types.Device{
		{ID: 1, DeviceID: DeviceCode("vc", types.DeviceTypePanel, 1), DeviceType: types.DeviceTypePanel},
		{ID: 2, DeviceID: DeviceCode("vc", types.DeviceTypeBattery, 1), DeviceType: types.DeviceTypeBattery},
	}}
	rows := &vcTelemetryRows{}
	log := zap.NewNop()
	s := NewTelemetryService(modems, devices, rows, nil, nil, hub.New(nil, log), log, time.Minute, 10*time.Minute)
	payload := map[string]any{"timestamp": at.Format(time.RFC3339), "pv": []any{map[string]any{"power": 0.0}}}
	if _, err := s.ProcessModemMessage(context.Background(), "vc", payload); err != nil {
		t.Fatal(err)
	}
	if len(rows.times) != 1 || !rows.times[0].Equal(at) || len(devices.saved) != 1 || !modems.online[0].Equal(at) {
		t.Fatal(rows.times, devices.saved, modems.online)
	}
	if rows.values[0].Power == nil || *rows.values[0].Power != 0 {
		t.Fatal("actual zero lost")
	}
	// The absent battery must neither get zero readings nor become online.
	if devices.saved[0].DeviceType != types.DeviceTypePanel {
		t.Fatal(devices.saved)
	}
	if _, err := s.ProcessModemMessage(context.Background(), "vc", payload); err != nil {
		t.Fatal(err)
	}
	if len(rows.times) != 1 || len(modems.online) != 1 {
		t.Fatal("unchanged snapshot inserted again")
	}
}

func TestValueCloudsIngestionRejectsStaleTimestampBeforeMutation(t *testing.T) {
	for _, timestamp := range []string{"", time.Now().Add(-11 * time.Minute).Format(time.RFC3339), time.Now().Add(2 * time.Minute).Format(time.RFC3339)} {
		modems := &vcTelemetryModems{modem: &types.Modem{Connection: map[string]any{"protocol": types.ProtocolValueClouds}}}
		s := NewTelemetryService(modems, nil, nil, nil, nil, nil, zap.NewNop(), time.Minute, time.Minute)
		if _, err := s.ProcessModemMessage(context.Background(), "vc", map[string]any{"timestamp": timestamp}); err == nil {
			t.Fatal("accepted", timestamp)
		}
		if len(modems.online) != 0 {
			t.Fatal("stale reading refreshed online status")
		}
	}
}

func TestLocalIngestionKeepsMissingSectionPolicy(t *testing.T) {
	modems := &vcTelemetryModems{modem: &types.Modem{ID: 1, Serial: "local", Status: types.StatusOnline}}
	devices := &vcTelemetryDevices{devices: []types.Device{
		{ID: 1, DeviceID: DeviceCode("local", types.DeviceTypePanel, 1), DeviceType: types.DeviceTypePanel},
		{ID: 2, DeviceID: DeviceCode("local", types.DeviceTypeLoad, 1), DeviceType: types.DeviceTypeLoad},
	}}
	rows := &vcTelemetryRows{}
	log := zap.NewNop()
	s := NewTelemetryService(modems, devices, rows, nil, nil, hub.New(nil, log), log, time.Minute, time.Minute)
	if _, err := s.ProcessModemMessage(context.Background(), "local", map[string]any{"pv": []any{map[string]any{"power": 50.0}}}); err != nil {
		t.Fatal(err)
	}
	if len(rows.values) != 2 || rows.values[1].Power == nil || *rows.values[1].Power != 0 {
		t.Fatal("legacy source behavior changed", rows.values)
	}
}
