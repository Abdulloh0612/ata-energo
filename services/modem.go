package services

import (
	"context"
	"fmt"
	"strings"

	"ata-energo/db/repositories"
	"ata-energo/types"
)

// Работа с модемами: какие устройства есть у модема и как их создать.
//
// У каждого модема устройства называются так (device_id):
//
//	<SN>-inv   инвертор           <SN>-bat   батарея
//	<SN>-pv1   панели (строка 1)  <SN>-pv2   панели (строка 2) ...
//	<SN>-grid  сеть               <SN>-load  нагрузка
//	<SN>-gen   генератор (только если он есть)

// Sections — разделы сообщения модема -> тип устройства (порядок важен).
var Sections = []struct {
	Name string
	Type string
}{
	{"inverter", types.DeviceTypeInverter},
	{"pv", types.DeviceTypePanel},
	{"battery", types.DeviceTypeBattery},
	{"grid", types.DeviceTypeGrid},
	{"generator", types.DeviceTypeGenerator},
	{"load", types.DeviceTypeLoad},
}

// SectionType — тип устройства раздела ("" — неизвестный раздел).
func SectionType(section string) string {
	for _, s := range Sections {
		if s.Name == section {
			return s.Type
		}
	}
	return ""
}

var suffix = map[string][2]string{
	types.DeviceTypeInverter:  {"inv", "Инвертор"},
	types.DeviceTypePanel:     {"pv", "Панели"},
	types.DeviceTypeBattery:   {"bat", "Батарея"},
	types.DeviceTypeGrid:      {"grid", "Сеть"},
	types.DeviceTypeGenerator: {"gen", "Генератор"},
	types.DeviceTypeLoad:      {"load", "Нагрузка"},
}

// StandardSections — стандартный набор разделов станции.
var StandardSections = []string{"inverter", "pv", "battery", "grid", "load"}

// DeviceCode собирает device_id, например DeviceCode("2710000001", "panel", 2) -> "2710000001-pv2".
func DeviceCode(serial, deviceType string, number int) string {
	s := suffix[deviceType][0]
	if deviceType == types.DeviceTypePanel {
		s += fmt.Sprint(number)
	}
	return serial + "-" + s
}

type ModemService struct {
	modems  repositories.ModemRepository
	devices repositories.DeviceRepository
}

func NewModemService(modems repositories.ModemRepository, devices repositories.DeviceRepository) *ModemService {
	return &ModemService{modems: modems, devices: devices}
}

// EnsureDevice находит устройство модема или создаёт его, если его ещё нет.
func (s *ModemService) EnsureDevice(ctx context.Context, m *types.Modem, deviceType string, number int) (*types.Device, error) {
	code := DeviceCode(m.Serial, deviceType, number)
	name := suffix[deviceType][1]
	if deviceType == types.DeviceTypePanel && number > 1 {
		name += fmt.Sprintf(" %d", number)
	}
	modemID := m.ID
	return s.devices.GetOrCreate(ctx, &types.Device{
		ClientID: m.ClientID, ModemID: &modemID, DeviceType: deviceType,
		Name: name, SerialNumber: code, DeviceID: code,
	})
}

// SyncModemDevices создаёт устройства модема. Генератор — только если он отмечен.
//
// m.Sections — какие разделы есть у станции (например, у сетевого инвертора без батареи:
// ["inverter", "pv"]). Пусто — стандартный набор. Если генератор убрали — удаляет его устройство.
// Если модем перенесли к другому клиенту — переносит и его устройства.
func (s *ModemService) SyncModemDevices(ctx context.Context, m *types.Modem) error {
	source := m.Sections
	if len(source) == 0 {
		source = StandardSections
	}
	var sections []string
	for _, sec := range source {
		if t := SectionType(sec); t != "" && sec != "generator" {
			sections = append(sections, sec)
		}
	}
	for _, sec := range sections {
		if _, err := s.EnsureDevice(ctx, m, SectionType(sec), 1); err != nil {
			return err
		}
	}
	if len(m.Sections) > 0 {
		keep := []string{types.DeviceTypeGenerator}
		for _, sec := range sections {
			keep = append(keep, SectionType(sec))
		}
		if err := s.devices.DeleteByModemExceptTypes(ctx, m.ID, keep); err != nil {
			return err
		}
	}
	if m.HasGenerator {
		if _, err := s.EnsureDevice(ctx, m, types.DeviceTypeGenerator, 1); err != nil {
			return err
		}
	} else if err := s.devices.DeleteByModemType(ctx, m.ID, types.DeviceTypeGenerator); err != nil {
		return err
	}
	return s.devices.ReassignModemClient(ctx, m.ID, m.ClientID)
}

// RenameModemDevices — если сменили SN модема, меняем начало device_id у его устройств.
func (s *ModemService) RenameModemDevices(ctx context.Context, m *types.Modem, oldSerial string) error {
	list, err := s.devices.ListByModem(ctx, m.ID)
	if err != nil {
		return err
	}
	for i := range list {
		d := &list[i]
		if strings.HasPrefix(d.DeviceID, oldSerial+"-") {
			d.DeviceID = m.Serial + d.DeviceID[len(oldSerial):]
			d.SerialNumber = d.DeviceID
			if err := s.devices.Update(ctx, d); err != nil {
				return err
			}
		}
	}
	return nil
}
