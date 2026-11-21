package types

import "time"

// Вендоры облачных API.
const VendorDeye = "deye"

// Протоколы чтения данных модема (Modem.Connection["protocol"]).
const ProtocolDeyeCloud = "deye_cloud"

// VendorAccount — учётная запись облака производителя (Deye Cloud OpenAPI).
//
// Пароль Deye требует в виде SHA-256, поэтому в базе храним только SHA-256 (не сам пароль).
type VendorAccount struct {
	ID              int64      `json:"id"`
	Vendor          string     `json:"vendor"`
	Name            string     `json:"name"`
	BaseURL         string     `json:"base_url"`
	AppID           string     `json:"-"`
	AppSecret       string     `json:"-"`
	Email           string     `json:"-"`
	Username        string     `json:"-"`
	Mobile          string     `json:"-"`
	CountryCode     string     `json:"-"`
	PasswordSHA256  string     `json:"-"`
	CompanyID       *int64     `json:"-"`
	IsActive        bool       `json:"is_active"`
	ProviderEnabled bool       `json:"-"`
	HasCredentials  bool       `json:"has_credentials"`
	LastError       string     `json:"last_error"`
	LastSyncAt      *time.Time `json:"last_sync_at"`
	CreatedAt       time.Time  `json:"created_at"`
	ModemsCount     int        `json:"modems_count"`
}

// VendorAccountWrite — тело POST/PATCH /api/deye/accounts/.
type VendorAccountWrite struct {
	Name        *string `json:"name"`
	BaseURL     *string `json:"base_url"`
	AppID       *string `json:"app_id"`
	AppSecret   *string `json:"app_secret"`
	Email       *string `json:"email"`
	Username    *string `json:"username"`
	Mobile      *string `json:"mobile"`
	CountryCode *string `json:"country_code"`
	Password    *string `json:"password"`
	CompanyID   *int64  `json:"company_id"`
	IsActive    *bool   `json:"is_active"`
}
