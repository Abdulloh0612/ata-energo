package deye

import (
	"fmt"
	"math"
)

// MapOptions — как переводить данные Deye в формат модема Ата Энерго.
//
// Знаки в формате модема: батарея + заряд / - разряд; сеть + покупка / - продажа.
// В точках устройства Deye батарея обычно "+ разряд", поэтому по умолчанию знак батареи
// из device/latest переворачивается. Если на схеме знак не совпадает с экраном инвертора —
// включите invert_battery / invert_grid в connection модема.
type MapOptions struct {
	InvertBattery bool
	InvertGrid    bool
	// StationPowerScale — множитель мощностей из station/latest (1 = Вт, 1000 = если облако отдаёт кВт).
	StationPowerScale float64
	HasGenerator      bool
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func part(power, voltage, current *float64) map[string]any {
	p := map[string]any{}
	if power != nil {
		p["power"] = round2(*power)
	}
	if voltage != nil {
		p["voltage"] = round2(*voltage)
	}
	if current != nil {
		p["current"] = round2(*current)
	}
	return p
}

func opt(v float64, ok bool) *float64 {
	if !ok {
		return nil
	}
	return &v
}

func neg(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := -*p
	return &v
}

// Кандидаты ключей (после NormKey) для разных моделей Deye (1ф/3ф гибриды, микроинверторы).
var (
	keysPVTotal   = []string{"totalsolarpower", "totaldcinputpower", "totalpvpower", "pvpower", "dcpower", "solarpower"}
	keysBatSOC    = []string{"soc", "batterysoc", "battsoc", "bmssoc"}
	keysBatPower  = []string{"batterypower", "totalbatterypower", "battpower", "batterychargedischargepower"}
	keysBatVolt   = []string{"batteryvoltage", "battvoltage", "bmsvoltage"}
	keysBatCurr   = []string{"batterycurrent", "battcurrent", "bmscurrent", "batterycurrent1"}
	keysBatTemp   = []string{"batterytemperature", "batttemperature", "temperaturebattery", "bmstemperature"}
	keysGridPower = []string{"totalgridpower", "gridpower", "totalgridactivepower", "gridactivepower", "externalcttotalpower", "meterpower"}
	keysGridPhase = []string{"gridpowerl1", "gridpowerl2", "gridpowerl3", "externalctpowerl1", "externalctpowerl2", "externalctpowerl3"}
	keysGridVolt  = []string{"gridvoltagel1", "gridvoltage", "gridvoltagel1n", "gridvoltagel1l2", "acvoltagergrid", "gridvoltagea"}
	keysGridCurr  = []string{"gridcurrentl1", "gridcurrent", "gridcurrenta"}
	keysLoadPower = []string{"totalconsumptionpower", "totalloadpower", "loadpower", "consumptionpower", "totalconsumption", "upsloadpower"}
	keysLoadVolt  = []string{"loadvoltagel1", "loadvoltage", "loadvoltagel1n", "upsvoltagel1"}
	keysLoadCurr  = []string{"loadcurrentl1", "loadcurrent"}
	keysGenPower  = []string{"genpower", "generatorpower", "totalgenpower", "genactivepower", "gentotalpower", "generatoractivepower"}
	keysGenVolt   = []string{"genvoltagel1", "genvoltage", "generatorvoltage", "generatorvoltagel1"}
	keysGenCurr   = []string{"gencurrentl1", "gencurrent", "generatorcurrent"}
	keysInvPower  = []string{"totalacoutputpower", "inverteroutputpower", "totaloutputpower", "acoutputpower", "inverterpower", "totalinverterpower", "acactivepower"}
	keysInvVolt   = []string{"invertervoltagel1", "acvoltagel1", "acvoltage", "outputvoltage", "acvoltagerua", "invertervoltage"}
	keysInvCurr   = []string{"invertercurrentl1", "accurrentl1", "accurrent", "outputcurrent", "invertercurrent"}
	keysInvTemp   = []string{"dctemperature", "actemperature", "radiatortemperature", "invertertemperature", "heatsinktemperature", "temperature"}
)

func pvKeys(n int, what string) []string {
	return []string{
		fmt.Sprintf("pv%d%s", n, what), fmt.Sprintf("dc%spv%d", what, n), fmt.Sprintf("dc%d%s", n, what),
		fmt.Sprintf("pv%s%d", what, n), fmt.Sprintf("mppt%d%s", n, what), fmt.Sprintf("dcinput%d%s", n, what),
	}
}

// BuildModemPayload превращает показания Deye (устройство и/или станция) в сообщение модема:
//
//	{"modem": SN, "inverter": {...}, "pv": [...], "battery": {...}, "grid": {...}, "load": {...}, "generator": {...}}
//
// dev или st могут быть nil. Значения устройства точнее (есть напряжения, токи, температура),
// данные станции — запасной вариант для мощностей и заряда.
func BuildModemPayload(serial string, dev *DeviceLatestData, st *StationLatestResponse, o MapOptions) map[string]any {
	p := Points{}
	if dev != nil {
		p = ToPoints(dev.DataList)
	}
	scale := o.StationPowerScale
	if scale == 0 {
		scale = 1
	}
	stv := func(v *float64) *float64 {
		if v == nil {
			return nil
		}
		x := *v * scale
		return &x
	}

	msg := map[string]any{"modem": serial}

	// ── Панели (строки MPPT) ──
	var pvParts []any
	var pvSum float64
	for n := 1; n <= 12; n++ {
		pw := opt(p.First(pvKeys(n, "power")...))
		v := opt(p.First(pvKeys(n, "voltage")...))
		c := opt(p.First(pvKeys(n, "current")...))
		if pw == nil && v == nil && c == nil {
			continue
		}
		if pw == nil && v != nil && c != nil {
			x := *v * *c
			pw = &x
		}
		if pw != nil {
			pvSum += *pw
		}
		pvParts = append(pvParts, part(pw, v, c))
	}
	if len(pvParts) == 0 {
		total := opt(p.First(keysPVTotal...))
		if total == nil && st != nil {
			total = stv(st.GenerationPower)
		}
		if total != nil {
			pvParts = []any{part(total, nil, nil)}
		}
	} else if total, ok := p.First(keysPVTotal...); ok && pvSum == 0 && total > 0 {
		// строки без мощностей, но есть общая — кладём её в первую строку
		pvParts[0].(map[string]any)["power"] = round2(total)
	}
	if len(pvParts) > 0 {
		msg["pv"] = pvParts
	}

	// ── Батарея ──
	batPower := opt(p.First(keysBatPower...))
	if batPower != nil {
		batPower = neg(batPower) // Deye: + разряд  ->  наш формат: + заряд
	} else if st != nil {
		switch {
		case st.ChargePower != nil || st.DischargePower != nil:
			var ch, dis float64
			if st.ChargePower != nil {
				ch = *st.ChargePower
			}
			if st.DischargePower != nil {
				dis = *st.DischargePower
			}
			x := (ch - dis) * scale
			batPower = &x
		case st.BatteryPower != nil:
			batPower = neg(stv(st.BatteryPower))
		}
	}
	if o.InvertBattery {
		batPower = neg(batPower)
	}
	soc := opt(p.First(keysBatSOC...))
	if soc == nil && st != nil {
		soc = st.BatterySOC
	}
	batCurr := opt(p.First(keysBatCurr...))
	if batCurr != nil && batPower != nil && (*batCurr > 0) != (*batPower > 0) && *batPower != 0 {
		batCurr = neg(batCurr) // ток — с тем же знаком, что и мощность
	}
	if batPower != nil || soc != nil {
		b := part(batPower, opt(p.First(keysBatVolt...)), batCurr)
		if soc != nil {
			b["soc"] = round2(*soc)
		}
		if t, ok := p.First(keysBatTemp...); ok {
			b["temperature"] = round2(t)
		}
		msg["battery"] = b
	}

	// ── Сеть ──
	gridPower := opt(p.First(keysGridPower...))
	if gridPower == nil {
		gridPower = opt(p.Sum(keysGridPhase...))
	}
	if gridPower == nil && st != nil {
		switch {
		case st.PurchasePower != nil || st.WirePower != nil:
			var buy, sell float64
			if st.PurchasePower != nil {
				buy = *st.PurchasePower
			}
			if st.WirePower != nil {
				sell = *st.WirePower
			}
			x := (buy - sell) * scale
			gridPower = &x
		case st.GridPower != nil:
			gridPower = stv(st.GridPower)
		}
	}
	if o.InvertGrid {
		gridPower = neg(gridPower)
	}
	gridVolt := opt(p.First(keysGridVolt...))
	if gridPower != nil || gridVolt != nil {
		msg["grid"] = part(gridPower, gridVolt, opt(p.First(keysGridCurr...)))
	}

	// ── Нагрузка ──
	loadPower := opt(p.First(keysLoadPower...))
	if loadPower == nil && st != nil {
		loadPower = stv(st.ConsumptionPower)
	}
	loadVolt := opt(p.First(keysLoadVolt...))
	if loadPower != nil || loadVolt != nil {
		msg["load"] = part(loadPower, loadVolt, opt(p.First(keysLoadCurr...)))
	}

	// ── Генератор (только если он есть или прислал ненулевые данные) ──
	genPower := opt(p.First(keysGenPower...))
	genVolt := opt(p.First(keysGenVolt...))
	if o.HasGenerator || (genPower != nil && *genPower != 0) {
		if genPower != nil || genVolt != nil {
			msg["generator"] = part(genPower, genVolt, opt(p.First(keysGenCurr...)))
		}
	}

	// ── Инвертор ──
	invPower := opt(p.First(keysInvPower...))
	if invPower == nil {
		invPower = loadPower
	}
	inv := part(invPower, opt(p.First(keysInvVolt...)), opt(p.First(keysInvCurr...)))
	if t, ok := p.First(keysInvTemp...); ok {
		inv["temperature"] = round2(t)
	}
	inv["error"] = nil
	if dev != nil && dev.DeviceState == DeviceStateAlert {
		inv["error"] = "Авария инвертора (Deye Cloud)"
	}
	msg["inverter"] = inv

	return msg
}
