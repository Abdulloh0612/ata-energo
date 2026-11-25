package types

import "time"

// Values — числа одного показания (nil = нет значения).
type Values struct {
	Current        *float64 `json:"current"`
	Voltage        *float64 `json:"voltage"`
	Power          *float64 `json:"power"`
	BatteryPercent *float64 `json:"battery_percent"`
	Temperature    *float64 `json:"temperature"`
}

// Telemetry — одно показание устройства в момент времени.
type Telemetry struct {
	ID         int64     `json:"id"`
	DeviceRef  int64     `json:"device"`
	DeviceCode string    `json:"device_id"`
	DeviceType string    `json:"device_type"`
	Timestamp  time.Time `json:"timestamp"`
	Values
	Error *string `json:"error"`
}

// HistoryPoint — точка графика.
type HistoryPoint struct {
	Timestamp time.Time `json:"timestamp"`
	Values
	Error *string `json:"error"`
}

// Виды аварий.
const (
	AlertKindOffline = "offline"
	AlertKindError   = "error"
)

// Alert — авария или пропажа связи.
type Alert struct {
	ID             int64     `json:"id"`
	DeviceRef      int64     `json:"device"`
	DeviceCode     string    `json:"device_code"`
	DeviceType     string    `json:"device_type"`
	ClientID       int64     `json:"client_id"`
	ClientName     string    `json:"client_name"`
	Kind           string    `json:"kind"`
	Text           string    `json:"text"`
	CreatedAt      time.Time `json:"created_at"`
	IsRead         bool      `json:"is_read"`
	ResponseStatus string    `json:"response_status"`
	ResponseUserID *int64    `json:"response_user_id"`
}

// AlertFilter — фильтры списка аварий.
type AlertFilter struct {
	OwnerUserID *int64
	Unread      bool
	ClientID    *int64
	Limit       int
}
