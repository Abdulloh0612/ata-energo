package types

import "time"

// Client — клиент компании (владелец солнечной станции).
type Client struct {
	ID        int64     `json:"id"`
	UserID    *int64    `json:"user"`
	Name      string    `json:"name"`
	Phone     string    `json:"phone"`
	Address   string    `json:"address"`
	CreatedAt time.Time `json:"created_at"`
}

// ClientModemShort — короткая информация о модеме в списке клиентов.
type ClientModemShort struct {
	ID           int64      `json:"id"`
	Serial       string     `json:"serial"`
	Name         string     `json:"name"`
	HasGenerator bool       `json:"has_generator"`
	Site         string     `json:"site"`
	Status       string     `json:"status"`
	LastSeen     *time.Time `json:"last_seen"`
}

// ClientView — клиент в JSON (+ логин, счётчики устройств, модемы).
type ClientView struct {
	Client
	Username     *string            `json:"username"`
	DevicesCount int                `json:"devices_count"`
	OnlineCount  int                `json:"online_count"`
	Modems       []ClientModemShort `json:"modems"`
}

// ClientWrite — тело запроса POST/PATCH /api/clients/.
//
// Можно сразу создать аккаунт для входа (login_username, login_password)
// и зарегистрировать модем (modem_serial, modem_name, has_generator).
type ClientWrite struct {
	Name          *string `json:"name"`
	Phone         *string `json:"phone"`
	Address       *string `json:"address"`
	LoginUsername *string `json:"login_username"`
	LoginPassword *string `json:"login_password"`
	ModemSerial   *string `json:"modem_serial"`
	ModemName     *string `json:"modem_name"`
	HasGenerator  *bool   `json:"has_generator"`
}
