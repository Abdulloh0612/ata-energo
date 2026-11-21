package services

import (
	"encoding/json"
	"testing"

	"ata-energo/types"
)

func TestToNumber(t *testing.T) {
	cases := []struct {
		in   any
		want *float64
	}{
		{nil, nil}, {"", nil}, {"abc", nil}, {1.234, ptr(1.23)}, {"230", ptr(230)},
		{json.Number("12.555"), ptr(12.56)}, {true, ptr(1)}, {[]any{1}, nil},
	}
	for _, c := range cases {
		got := ToNumber(c.in)
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("ToNumber(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValuesFromBatterySOC(t *testing.T) {
	v := valuesFrom(map[string]any{"power": -800.0, "soc": 80.0}, types.DeviceTypeBattery)
	if v.BatteryPercent == nil || *v.BatteryPercent != 80 || *v.Power != -800 {
		t.Fatalf("%+v", v)
	}
	v = valuesFrom(map[string]any{"soc": 80.0}, types.DeviceTypeInverter)
	if v.BatteryPercent != nil {
		t.Fatal("soc must be used only for battery")
	}
}

func TestErrorTextTruthy(t *testing.T) {
	if errorText(nil) != nil || errorText("") != nil || errorText(false) != nil || errorText(0.0) != nil {
		t.Fatal("falsy errors must be nil")
	}
	if e := errorText("F35"); e == nil || *e != "F35" {
		t.Fatal(e)
	}
}

func TestDeviceCode(t *testing.T) {
	if DeviceCode("2710000001", types.DeviceTypePanel, 2) != "2710000001-pv2" ||
		DeviceCode("SN", types.DeviceTypeInverter, 1) != "SN-inv" ||
		DeviceCode("SN", types.DeviceTypeGenerator, 1) != "SN-gen" {
		t.Fatal("device codes")
	}
}

func TestModemSerialFor(t *testing.T) {
	d := &types.FoundDevice{MAC: "aa:bb:cc:dd:ee:ff", IP: "192.168.1.5", CheckResult: map[string]any{}}
	if ModemSerialFor(d) != "LAN-AABBCCDDEEFF" {
		t.Fatal(ModemSerialFor(d))
	}
	d.CheckResult["serial"] = 2712345678.0
	if ModemSerialFor(d) != "2712345678" {
		t.Fatal(ModemSerialFor(d))
	}
}
