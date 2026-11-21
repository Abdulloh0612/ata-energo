package types

import "time"

// AgentOnlineSeconds — агент считается "в сети", если выходил на связь за последние 30 сек.
const AgentOnlineSeconds = 30

// Agent — локальный агент (agent/ata_agent.py) в сети клиента. Вход по токену (в базе только хэш).
type Agent struct {
	ID           int64          `json:"id"`
	Name         string         `json:"name"`
	ClientID     *int64         `json:"client"`
	ClientName   *string        `json:"client_name"`
	TokenHash    string         `json:"-"`
	TokenHint    string         `json:"token_hint"`
	Hostname     string         `json:"hostname"`
	IP           string         `json:"ip"`
	LocalIP      string         `json:"local_ip"`
	Platform     string         `json:"platform"`
	Version      string         `json:"version"`
	Capabilities map[string]any `json:"capabilities"`
	LastSeen     *time.Time     `json:"last_seen"`
	Online       bool           `json:"online"`
	IsActive     bool           `json:"is_active"`
	CreatedAt    time.Time      `json:"created_at"`
}

// ComputeOnline — агент на связи?
func (a *Agent) ComputeOnline() {
	a.Online = a.LastSeen != nil && time.Since(*a.LastSeen) < AgentOnlineSeconds*time.Second
}

// AgentWrite — тело POST/PATCH /api/agents/.
type AgentWrite struct {
	Name     *string `json:"name"`
	ClientID *int64  `json:"client"`
	IsActive *bool   `json:"is_active"`
}

// Виды, способы и статусы задач агента.
const (
	TaskKindScan     = "scan"
	TaskKindProbe    = "probe"
	TaskKindBLECheck = "ble_check"

	TaskMethodWiFi      = "wifi"
	TaskMethodBluetooth = "bluetooth"
	TaskMethodAll       = "all"

	TaskStatusPending = "pending"
	TaskStatusRunning = "running"
	TaskStatusDone    = "done"
	TaskStatusError   = "error"
)

// AgentTask — задача для агента: поиск (scan), проверка устройства (probe, ble_check).
type AgentTask struct {
	ID          int64          `json:"id"`
	AgentID     int64          `json:"agent"`
	AgentOnline bool           `json:"agent_online"`
	Kind        string         `json:"kind"`
	Method      string         `json:"method"`
	Params      map[string]any `json:"params"`
	DeviceID    *int64         `json:"device"`
	Status      string         `json:"status"`
	Result      map[string]any `json:"result"`
	Error       string         `json:"error"`
	Log         string         `json:"log"`
	CreatedBy   *int64         `json:"-"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at"`
	FinishedAt  *time.Time     `json:"finished_at"`
	Found       []FoundDevice  `json:"found"`
}

// Типы и статусы найденных устройств.
const (
	FoundKindInverter = "inverter"
	FoundKindLogger   = "logger"
	FoundKindUnknown  = "unknown"

	FoundStatusFound      = "found"
	FoundStatusChecking   = "checking"
	FoundStatusCompatible = "compatible"
	FoundStatusNeedsSetup = "needs_setup"
	FoundStatusConnected  = "connected"
	FoundStatusError      = "error"
)

var FoundKindDisplay = map[string]string{
	FoundKindInverter: "Инвертор",
	FoundKindLogger:   "Logger (модуль связи)",
	FoundKindUnknown:  "Неизвестное устройство",
}

var FoundStatusDisplay = map[string]string{
	FoundStatusFound:      "Обнаружено",
	FoundStatusChecking:   "Проверка…",
	FoundStatusCompatible: "Показания получены",
	FoundStatusNeedsSetup: "Устройство обнаружено, требуется настройка протокола",
	FoundStatusConnected:  "Подключено к мониторингу",
	FoundStatusError:      "Ошибка проверки",
}

// FoundDevice — реально обнаруженное устройство (Wi-Fi/LAN или Bluetooth).
type FoundDevice struct {
	ID             int64          `json:"id"`
	AgentID        *int64         `json:"agent"`
	AgentName      *string        `json:"agent_name"`
	TaskID         *int64         `json:"task"`
	Source         string         `json:"source"`
	Key            string         `json:"key"`
	ConnectionType string         `json:"connection_type"`
	Name           string         `json:"name"`
	Manufacturer   string         `json:"manufacturer"`
	Model          string         `json:"model"`
	Serial         string         `json:"serial"`
	IP             string         `json:"ip"`
	MAC            string         `json:"mac"`
	BTAddress      string         `json:"bt_address"`
	RSSI           *int           `json:"rssi"`
	DeviceKind     string         `json:"device_kind"`
	KindDisplay    string         `json:"kind_display"`
	Protocols      []string       `json:"protocols"`
	Details        map[string]any `json:"details"`
	Status         string         `json:"status"`
	StatusDisplay  string         `json:"status_display"`
	CheckResult    map[string]any `json:"check_result"`
	ModemID        *int64         `json:"modem"`
	ModemSerial    *string        `json:"modem_serial"`
	ModemClient    *int64         `json:"modem_client"`
	FirstSeen      time.Time      `json:"first_seen"`
	LastSeen       time.Time      `json:"last_seen"`
}

// FillDisplay — заполняет подписи типа и статуса.
func (d *FoundDevice) FillDisplay() {
	d.KindDisplay = FoundKindDisplay[d.DeviceKind]
	d.StatusDisplay = FoundStatusDisplay[d.Status]
}
