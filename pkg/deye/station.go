package deye

import "context"

// ─── Station ─────────────────────────────────────────────────────────────────

type PageRequest struct {
	Page int `json:"page,omitempty"`
	Size int `json:"size,omitempty"` // station/list: max 200
}

type Station struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	LocationLat             *float64   `json:"locationLat"`
	LocationLng             *float64   `json:"locationLng"`
	LocationAddress         string     `json:"locationAddress"`
	RegionNationID          *int64     `json:"regionNationId"`
	RegionTimezone          string     `json:"regionTimezone"`
	GridInterconnectionType string     `json:"gridInterconnectionType"`
	InstalledCapacity       *float64   `json:"installedCapacity"`
	StartOperatingTime      FlexString `json:"startOperatingTime"`
	CreatedDate             FlexString `json:"createdDate"`
	BatterySOC              *float64   `json:"batterySOC"`
	ConnectionStatus        string     `json:"connectionStatus"` // NORMAL, NO_DEVICE, ALL_OFFLINE, PARTIAL_OFFLINE
	GenerationPower         *float64   `json:"generationPower"`
	LastUpdateTime          FlexString `json:"lastUpdateTime"`
	ContactPhone            string     `json:"contactPhone"`
	OwnerName               string     `json:"ownerName"`
	CountryCode             string     `json:"countryCode"`
}

type StationListResponse struct {
	BaseResponse
	Total       int       `json:"total"`
	StationList []Station `json:"stationList"`
}

// StationList — POST /v1.0/station/list: станции аккаунта.
func (c *Client) StationList(ctx context.Context, req PageRequest) (*StationListResponse, error) {
	var out StationListResponse
	return &out, c.post(ctx, "/v1.0/station/list", req, &out)
}

type StationDetailResponse struct {
	BaseResponse
	Station *Station `json:"station"`
}

// StationDetail — POST /v1.0/station/detail.
func (c *Client) StationDetail(ctx context.Context, stationID int64) (*StationDetailResponse, error) {
	var out StationDetailResponse
	return &out, c.post(ctx, "/v1.0/station/detail", map[string]any{"stationId": stationID}, &out)
}

type DeviceListItem struct {
	DeviceID       int64   `json:"deviceId"`
	DeviceSn       string  `json:"deviceSn"`
	DeviceType     string  `json:"deviceType"`
	ProductID      string  `json:"productId"` // 0_5412_1 — 3ф HV гибрид, 0_5411_1 — 3ф LV, 0_5407_1 — 1ф гибрид
	StationID      int64   `json:"stationId"`
	ConnectStatus  FlexInt `json:"connectStatus"` // 0 offline, 1 online, 2 alert
	CollectionTime FlexInt `json:"collectionTime"`
	ParentID       *int64  `json:"parentId"`
}

type StationWithDevices struct {
	ID                      int64            `json:"id"`
	Name                    string           `json:"name"`
	LocationLat             *float64         `json:"locationLat"`
	LocationLng             *float64         `json:"locationLng"`
	LocationAddress         string           `json:"locationAddress"`
	RegionNationID          *int64           `json:"regionNationId"`
	RegionTimezone          string           `json:"regionTimezone"`
	GridInterconnectionType string           `json:"gridInterconnectionType"`
	InstalledCapacity       *float64         `json:"installedCapacity"`
	StartOperatingTime      FlexString       `json:"startOperatingTime"`
	CreatedDate             FlexString       `json:"createdDate"`
	ContactPhone            string           `json:"contactPhone"`
	OwnerName               string           `json:"ownerName"`
	Type                    string           `json:"type"`
	DeviceTotal             int              `json:"deviceTotal"`
	DeviceListItems         []DeviceListItem `json:"deviceListItems"`
}

type StationWithDeviceRequest struct {
	Page       int    `json:"page,omitempty"`
	Size       int    `json:"size,omitempty"` // max 50
	DeviceType string `json:"deviceType,omitempty"`
}

type StationWithDeviceResponse struct {
	BaseResponse
	StationTotal int                  `json:"stationTotal"`
	StationList  []StationWithDevices `json:"stationList"`
}

// StationListWithDevice — POST /v1.0/station/listWithDevice: станции вместе с устройствами.
func (c *Client) StationListWithDevice(ctx context.Context, req StationWithDeviceRequest) (*StationWithDeviceResponse, error) {
	var out StationWithDeviceResponse
	return &out, c.post(ctx, "/v1.0/station/listWithDevice", req, &out)
}

type StationDeviceRequest struct {
	StationIDs []int64 `json:"stationIds"` // до 10 станций
	Page       int     `json:"page,omitempty"`
	Size       int     `json:"size,omitempty"` // max 200
}

type StationDeviceResponse struct {
	BaseResponse
	Total           int              `json:"total"`
	DeviceListItems []DeviceListItem `json:"deviceListItems"`
}

// StationDevices — POST /v1.0/station/device: устройства станций (до 10 станций за раз).
func (c *Client) StationDevices(ctx context.Context, req StationDeviceRequest) (*StationDeviceResponse, error) {
	var out StationDeviceResponse
	return &out, c.post(ctx, "/v1.0/station/device", req, &out)
}

type StationLatestResponse struct {
	BaseResponse
	GenerationPower    *float64   `json:"generationPower"`
	ConsumptionPower   *float64   `json:"consumptionPower"`
	GridPower          *float64   `json:"gridPower"`
	PurchasePower      *float64   `json:"purchasePower"`
	WirePower          *float64   `json:"wirePower"`
	ChargePower        *float64   `json:"chargePower"`
	DischargePower     *float64   `json:"dischargePower"`
	BatteryPower       *float64   `json:"batteryPower"`
	BatterySOC         *float64   `json:"batterySOC"`
	IrradiateIntensity *float64   `json:"irradiateIntensity"`
	LastUpdateTime     FlexString `json:"lastUpdateTime"`
}

// StationLatest — POST /v1.0/station/latest: текущие мощности станции.
func (c *Client) StationLatest(ctx context.Context, stationID int64) (*StationLatestResponse, error) {
	var out StationLatestResponse
	return &out, c.post(ctx, "/v1.0/station/latest", map[string]any{"stationId": stationID}, &out)
}

// StationHistoryRequest — granularity: 1 — кадры за день (startAt yyyy-MM-dd), 2 — дни (до 31),
// 3 — месяцы (yyyy-MM, до 12), 4 — годы (yyyy).
type StationHistoryRequest struct {
	StationID   int64  `json:"stationId"`
	Granularity int    `json:"granularity"`
	StartAt     string `json:"startAt"`
	EndAt       string `json:"endAt,omitempty"`
}

type StationDataItem struct {
	GenerationPower           *float64   `json:"generationPower"`
	ConsumptionPower          *float64   `json:"consumptionPower"`
	GridPower                 *float64   `json:"gridPower"`
	PurchasePower             *float64   `json:"purchasePower"`
	WirePower                 *float64   `json:"wirePower"`
	ChargePower               *float64   `json:"chargePower"`
	DischargePower            *float64   `json:"dischargePower"`
	BatteryPower              *float64   `json:"batteryPower"`
	BatterySOC                *float64   `json:"batterySOC"`
	IrradiateIntensity        *float64   `json:"irradiateIntensity"`
	GenerationValue           *float64   `json:"generationValue"`
	GenerationRatio           *float64   `json:"generationRatio"`
	GridValue                 *float64   `json:"gridValue"`
	GridRatio                 *float64   `json:"gridRatio"`
	ConsumptionValue          *float64   `json:"consumptionValue"`
	ConsumptionRatio          *float64   `json:"consumptionRatio"`
	ConsumptionDischargeRatio *float64   `json:"consumptionDischargeRatio"`
	ChargeValue               *float64   `json:"chargeValue"`
	ChargeRatio               *float64   `json:"chargeRatio"`
	DischargeValue            *float64   `json:"dischargeValue"`
	PurchaseValue             *float64   `json:"purchaseValue"`
	PurchaseRatio             *float64   `json:"purchaseRatio"`
	FullPowerHours            *float64   `json:"fullPowerHours"`
	Irradiate                 *float64   `json:"irradiate"`
	TheoreticalGeneration     *float64   `json:"theoreticalGeneration"`
	PR                        *float64   `json:"pr"`
	CPR                       *float64   `json:"cpr"`
	TimeStamp                 FlexString `json:"timeStamp"`
	Year                      *int       `json:"year,omitempty"`
	Month                     *int       `json:"month,omitempty"`
	Day                       *int       `json:"day,omitempty"`
}

type StationHistoryResponse struct {
	BaseResponse
	Total            int               `json:"total"`
	StationDataItems []StationDataItem `json:"stationDataItems"`
}

// StationHistory — POST /v1.0/station/history.
func (c *Client) StationHistory(ctx context.Context, req StationHistoryRequest) (*StationHistoryResponse, error) {
	var out StationHistoryResponse
	return &out, c.post(ctx, "/v1.0/station/history", req, &out)
}

// StationHistoryPowerRequest — unix-секунды (10 цифр), диапазон до 12 месяцев.
type StationHistoryPowerRequest struct {
	StationID      int64 `json:"stationId"`
	StartTimestamp int64 `json:"startTimestamp"`
	EndTimestamp   int64 `json:"endTimestamp"`
}

// StationHistoryPower — POST /v1.0/station/history/power.
func (c *Client) StationHistoryPower(ctx context.Context, req StationHistoryPowerRequest) (*StationHistoryResponse, error) {
	var out StationHistoryResponse
	return &out, c.post(ctx, "/v1.0/station/history/power", req, &out)
}

// StationAlertListRequest — unix-секунды, диапазон не больше 180 дней.
type StationAlertListRequest struct {
	StationID      int64 `json:"stationId"`
	StartTimestamp int64 `json:"startTimestamp"`
	EndTimestamp   int64 `json:"endTimestamp"`
	Page           int   `json:"page,omitempty"`
	Size           int   `json:"size,omitempty"` // max 200
}

type AlertItem struct {
	AlertID        string     `json:"alertId"`
	AlertCode      string     `json:"alertCode"`
	AlertName      string     `json:"alertName"`
	ProtocolName   string     `json:"protocolName"`
	Level          int        `json:"level"`  // 0 Notice, 1 Warning, 2 Failure
	Impact         int        `json:"impact"` // 0 нет, 1 выработка, 2 безопасность, 3 оба
	Status         int        `json:"status"` // 1 открыта, 0 закрыта
	AlertStartTime FlexInt    `json:"alertStartTime"`
	AlertEndTime   FlexInt    `json:"alertEndTime"`
	DeviceID       FlexString `json:"deviceId,omitempty"`
	DeviceSn       string     `json:"deviceSn,omitempty"`
	DeviceType     string     `json:"deviceType,omitempty"`
	Description    string     `json:"description,omitempty"`
	Reason         string     `json:"reason,omitempty"`
	Solution       string     `json:"solution,omitempty"`
}

type StationAlertListResponse struct {
	BaseResponse
	Total             int         `json:"total"`
	StationAlertItems []AlertItem `json:"stationAlertItems"`
}

// StationAlertList — POST /v1.0/station/alertList.
func (c *Client) StationAlertList(ctx context.Context, req StationAlertListRequest) (*StationAlertListResponse, error) {
	var out StationAlertListResponse
	return &out, c.post(ctx, "/v1.0/station/alertList", req, &out)
}

type Region struct {
	CountryCode string `json:"countryCode"` // ISO 3166-1 alpha-2
	Timezone    string `json:"timezone"`    // IANA, например Asia/Bishkek
	NationID    *int64 `json:"nationId,omitempty"`
	Level1      *int64 `json:"level1,omitempty"`
	Level2      *int64 `json:"level2,omitempty"`
	Level3      *int64 `json:"level3,omitempty"`
	Level4      *int64 `json:"level4,omitempty"`
	Level5      *int64 `json:"level5,omitempty"`
}

type StationCreateRequest struct {
	Name                     string   `json:"name"`
	LocationAddress          string   `json:"locationAddress"`
	LocationLat              float64  `json:"locationLat"`
	LocationLng              float64  `json:"locationLng"`
	InstalledCapacity        float64  `json:"installedCapacity"`
	Currency                 string   `json:"currency"`                // ISO 4217, например KGS / USD
	GridInterconnectionType  string   `json:"gridInterconnectionType"` // DISTRIBUTED_FULLY, EXCESS, OFF_GRID, BATTERY_BACKUP, ...
	Region                   Region   `json:"region"`
	Type                     string   `json:"type,omitempty"` // HOUSE_ROOF, COMMERCIAL_ROOF, INDUSTRIAL_ROOF, GROUND
	ContactPhone             string   `json:"contactPhone,omitempty"`
	OwnerName                string   `json:"ownerName,omitempty"`
	OwnerCompany             string   `json:"ownerCompany,omitempty"`
	MergeElectricPrice       *float64 `json:"mergeElectricPrice,omitempty"`
	ConstructionCost         *float64 `json:"constructionCost,omitempty"`
	InstallationAzimuthAngle *float64 `json:"installationAzimuthAngle,omitempty"`
	InstallationTiltAngle    *float64 `json:"installationTiltAngle,omitempty"`
	StartOperatingTime       string   `json:"startOperatingTime,omitempty"`
	StationImage             string   `json:"stationImage,omitempty"`
}

type StationCreateResponse struct {
	BaseResponse
	ID int64 `json:"id"`
}

// StationCreate — POST /v1.0/station/create.
func (c *Client) StationCreate(ctx context.Context, req StationCreateRequest) (*StationCreateResponse, error) {
	var out StationCreateResponse
	return &out, c.post(ctx, "/v1.0/station/create", req, &out)
}
