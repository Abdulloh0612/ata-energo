package types

import "time"

// Статусы связи (у модемов и устройств одинаковые).
const (
	StatusOnline  = "online"
	StatusOffline = "offline"
)

// Типы оборудования.
const (
	DeviceTypePanel     = "panel"
	DeviceTypeInverter  = "inverter"
	DeviceTypeBattery   = "battery"
	DeviceTypeGenerator = "generator"
	DeviceTypeGrid      = "grid"
	DeviceTypeLoad      = "load"
)

var DeviceTypeDisplay = map[string]string{
	DeviceTypePanel:     "Солнечная панель",
	DeviceTypeInverter:  "Инвертор",
	DeviceTypeBattery:   "Батарея",
	DeviceTypeGenerator: "Генератор",
	DeviceTypeGrid:      "Сеть",
	DeviceTypeLoad:      "Нагрузка",
}

func ValidDeviceType(t string) bool {
	_, ok := DeviceTypeDisplay[t]
	return ok
}

// Modem — модем инвертора. Его серийный номер (SN) написан на наклейке под QR-кодом.
// Модем присылает ОДНО сообщение со всеми данными станции.
type Modem struct {
	ID           int64  `json:"id"`
	ClientID     int64  `json:"client"`
	Serial       string `json:"serial"`
	Name         string `json:"name"`
	HasGenerator bool   `json:"has_generator"`
	Site         string `json:"site"`
	// Если данные читает локальный агент (а не модем шлёт MQTT сам)
	AgentID *int64 `json:"agent"`
	// Как читаются данные: {"protocol": "solarman_v5", "host": ..., "port": 8899, ...}
	// или {"protocol": "deye_cloud", "account_id": 1, "device_sn": "...", "station_id": 322}
	Connection map[string]any `json:"connection"`
	// Какие разделы есть у станции (пусто = стандартный набор)
	Sections  []string   `json:"sections"`
	Status    string     `json:"status"`
	LastSeen  *time.Time `json:"last_seen"`
	CreatedAt time.Time  `json:"created_at"`

	ClientName string `json:"client_name"`
}

// Protocol — протокол чтения данных из connection.
func (m *Modem) Protocol() string {
	if m == nil || m.Connection == nil {
		return ""
	}
	p, _ := m.Connection["protocol"].(string)
	return p
}

// ModemView — модем в JSON.
type ModemView struct {
	Modem
	AgentName    *string `json:"agent_name"`
	DevicesCount int     `json:"devices_count"`
}

// ModemWrite — тело POST/PATCH /api/modems/.
type ModemWrite struct {
	ClientID     *int64  `json:"client"`
	Serial       *string `json:"serial"`
	Name         *string `json:"name"`
	HasGenerator *bool   `json:"has_generator"`
	Site         *string `json:"site"`
}

// ModemFilter — фильтры списка модемов. OwnerUserID != nil — только модемы этого клиента-пользователя.
type ModemFilter struct {
	OwnerUserID *int64
	ClientID    *int64
	Search      string
}

// Device — одно устройство клиента. DeviceID — это id, который присылает модем.
type Device struct {
	ID           int64          `json:"id"`
	ClientID     int64          `json:"client"`
	ModemID      *int64         `json:"modem"`
	DeviceType   string         `json:"device_type"`
	Name         string         `json:"name"`
	SerialNumber string         `json:"serial_number"`
	DeviceID     string         `json:"device_id"`
	Status       string         `json:"status"`
	LastSeen     *time.Time     `json:"last_seen"`
	LastData     map[string]any `json:"last_data"`
	CreatedAt    time.Time      `json:"created_at"`

	ClientName  string  `json:"client_name"`
	ModemSerial *string `json:"modem_serial"`
}

// DeviceView — устройство в JSON (с понятной подписью типа).
type DeviceView struct {
	Device
	TypeDisplay string `json:"type_display"`
}

func NewDeviceView(d Device) DeviceView {
	return DeviceView{Device: d, TypeDisplay: DeviceTypeDisplay[d.DeviceType]}
}

// DeviceWrite — тело POST/PATCH /api/devices/.
type DeviceWrite struct {
	ClientID     *int64  `json:"client"`
	ModemID      *int64  `json:"modem"`
	DeviceType   *string `json:"device_type"`
	Name         *string `json:"name"`
	SerialNumber *string `json:"serial_number"`
	DeviceID     *string `json:"device_id"`
}

// DeviceFilter — фильтры списка устройств.
type DeviceFilter struct {
	OwnerUserID *int64
	ClientID    *int64
	Type        string
	Status      string
}

// DeviceInfo — короткая информация об устройстве для WebSocket-сообщения.
type DeviceInfo struct {
	ID         int64  `json:"id"`
	DeviceID   string `json:"device_id"`
	DeviceType string `json:"device_type"`
	Name       string `json:"name"`
	ClientID   int64  `json:"client_id"`
	ModemID    *int64 `json:"modem_id"`
	Status     string `json:"status"`
}

func (d *Device) Info() DeviceInfo {
	return DeviceInfo{
		ID: d.ID, DeviceID: d.DeviceID, DeviceType: d.DeviceType, Name: d.Name,
		ClientID: d.ClientID, ModemID: d.ModemID, Status: d.Status,
	}
}

// ModemInfo — короткая информация о модеме для WebSocket-сообщения.
type ModemInfo struct {
	ID           int64      `json:"id"`
	Serial       string     `json:"serial"`
	ClientID     int64      `json:"client_id"`
	Status       string     `json:"status"`
	LastSeen     *time.Time `json:"last_seen"`
	HasGenerator bool       `json:"has_generator"`
}

func (m *Modem) Info() ModemInfo {
	return ModemInfo{
		ID: m.ID, Serial: m.Serial, ClientID: m.ClientID, Status: m.Status,
		LastSeen: m.LastSeen, HasGenerator: m.HasGenerator,
	}
}
