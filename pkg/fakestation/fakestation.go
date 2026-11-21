// Package fakestation — фейковая солнечная станция для проверки без реального инвертора.
//
//	station := fakestation.New(true, 2)
//	station.ModemPayload("2710000001", 2)  -> одно сообщение модема со ВСЕМИ данными
//
// Знаки мощности: батарея + заряд / - разряд; сеть + покупка / - продажа.
package fakestation

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

const (
	PanelMaxW   = 6000.0  // максимальная мощность всех панелей
	BatteryWh   = 10000.0 // ёмкость батареи, Вт*ч
	BatteryMaxW = 3000.0  // максимальная мощность заряда/разряда
	DaySeconds  = 600.0   // "сутки" в симуляции длятся 10 минут — так интереснее смотреть
)

// Station — 2 строки панелей, инвертор, батарея, сеть, нагрузка и (если есть) генератор.
type Station struct {
	HasGenerator bool
	PVStrings    int
	Battery      float64 // заряд, %
	GridOn       bool
	hour         float64
}

func New(hasGenerator bool, pvStrings int) *Station {
	if pvStrings <= 0 {
		pvStrings = 2
	}
	now := time.Now()
	return &Station{
		HasGenerator: hasGenerator,
		PVStrings:    pvStrings,
		Battery:      uniform(45, 90),
		GridOn:       true,
		hour:         float64(now.Hour()) + float64(now.Minute())/60, // стартуем с текущего часа
	}
}

func uniform(a, b float64) float64 { return a + rand.Float64()*(b-a) }

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}

// sun — сила солнца от 0 до 1 в зависимости от часа (ночью 0).
func (s *Station) sun() float64 {
	if s.hour >= 6 && s.hour <= 19 {
		return math.Sin(math.Pi * (s.hour - 6) / 13)
	}
	return 0
}

type powers struct{ pv, load, battery, grid, generator float64 }

// step — шаг симуляции на dt секунд.
func (s *Station) step(dt float64) powers {
	simHours := dt * 24 / DaySeconds
	s.hour = math.Mod(s.hour+simHours, 24)

	pv := PanelMaxW * s.sun() * uniform(0.85, 1.0)
	load := 900 + uniform(0, 1400)
	if rand.Float64() < 0.1 {
		load += uniform(500, 1500)
	}

	// Иногда сеть пропадает (и потом возвращается)
	if s.GridOn && rand.Float64() < 0.01 {
		s.GridOn = false
	} else if !s.GridOn && rand.Float64() < 0.08 {
		s.GridOn = true
	}

	var batteryW, gridW, genW float64
	net := pv - load
	if net >= 0 {
		if s.Battery < 100 {
			batteryW = math.Min(net, BatteryMaxW)
		}
		if s.GridOn {
			gridW = -(net - batteryW)
		}
	} else {
		need := -net
		if s.Battery > 20 {
			batteryW = -math.Min(need, BatteryMaxW)
			need += batteryW
		}
		if need > 0 {
			if s.GridOn {
				gridW = need
			} else if s.HasGenerator {
				genW = need
			}
		}
	}
	s.Battery = math.Max(0, math.Min(100, s.Battery+batteryW*simHours/BatteryWh*100))
	return powers{pv: pv, load: load, battery: batteryW, grid: gridW, generator: genW}
}

// part — один раздел сообщения: мощность, напряжение, ток (+ доп. поля).
func part(power, voltage float64, extra map[string]any) map[string]any {
	if voltage != 0 {
		voltage = round(voltage+uniform(-2, 2), 1)
	}
	current := 0.0
	if voltage != 0 {
		current = round(power/voltage, 2)
	}
	p := map[string]any{"power": round(power, 1), "voltage": voltage, "current": current}
	for k, v := range extra {
		p[k] = v
	}
	return p
}

// ModemPayload делает шаг и возвращает сообщение модема в формате сервера.
func (s *Station) ModemPayload(serial string, dt float64) map[string]any {
	p := s.step(dt)
	pvParts := make([]any, 0, s.PVStrings)
	for i := 0; i < s.PVStrings; i++ {
		share := p.pv / float64(s.PVStrings) * uniform(0.95, 1.05)
		v := 0.0
		if share > 5 {
			v = 360 + uniform(-10, 20)
		}
		pvParts = append(pvParts, part(share, v, nil))
	}
	var batteryErr any
	if s.Battery < 15 {
		batteryErr = "Низкий заряд батареи"
	}
	gridV := 0.0
	if s.GridOn {
		gridV = 230
	}
	msg := map[string]any{
		"modem":    serial,
		"inverter": part(p.load, 230, map[string]any{"temperature": round(35+p.load/250, 1), "error": nil}),
		"pv":       pvParts,
		"battery": part(p.battery, 51.2, map[string]any{
			"soc": round(s.Battery, 1), "temperature": round(24+math.Abs(p.battery)/400, 1), "error": batteryErr,
		}),
		"grid": part(p.grid, gridV, nil),
		"load": part(p.load, 230, nil),
	}
	if s.HasGenerator {
		genV := 0.0
		if p.generator > 0 {
			genV = 230
		}
		msg["generator"] = part(p.generator, genV, nil)
	}
	return msg
}

// SeedModemSerial — SN модема тестового клиента №index (как в seed): 2710000001, 2710000002...
func SeedModemSerial(index int) string {
	return fmt.Sprintf("27100000%02d", index)
}

// SeedHasGenerator — у тестовых клиентов с нечётным номером есть генератор, у чётных — нет.
func SeedHasGenerator(index int) bool {
	return index%2 == 1
}
