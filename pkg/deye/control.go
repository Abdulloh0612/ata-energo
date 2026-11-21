package deye

import (
	"context"
	"fmt"
	"net/http"
)

// ─── Configuration (чтение настроек инвертора) ───────────────────────────────

type BatteryConfigResponse struct {
	BaseResponse
	BattCapacity         *int `json:"battCapacity"`
	BattLowCapacity      *int `json:"battLowCapacity"`
	BattShutDownCapacity *int `json:"battShutDownCapacity"`
	MaxChargeCurrent     *int `json:"maxChargeCurrent"`
	MaxDischargeCurrent  *int `json:"maxDischargeCurrent"`
}

// ConfigBattery — POST /v1.0/config/battery.
func (c *Client) ConfigBattery(ctx context.Context, deviceSn string) (*BatteryConfigResponse, error) {
	var out BatteryConfigResponse
	return &out, c.post(ctx, "/v1.0/config/battery", DeviceSnRequest{DeviceSn: deviceSn}, &out)
}

type SystemConfigResponse struct {
	BaseResponse
	SystemWorkMode  string `json:"systemWorkMode"`
	EnergyPattern   string `json:"energyPattern"`
	MaxSellPower    *int   `json:"maxSellPower"`
	MaxSolarPower   *int   `json:"maxSolarPower"`
	ZeroExportPower *int   `json:"zeroExportPower"`
}

// ConfigSystem — POST /v1.0/config/system.
func (c *Client) ConfigSystem(ctx context.Context, deviceSn string) (*SystemConfigResponse, error) {
	var out SystemConfigResponse
	return &out, c.post(ctx, "/v1.0/config/system", DeviceSnRequest{DeviceSn: deviceSn}, &out)
}

// TimeUseItem — одна из 6 строк расписания TOU. Время — "02:00", шаг 5 минут.
type TimeUseItem struct {
	Time             string `json:"time"`
	Power            int    `json:"power"`
	SOC              *int   `json:"soc,omitempty"`
	Voltage          *int   `json:"voltage,omitempty"`
	EnableGeneration bool   `json:"enableGeneration"`
	EnableGridCharge bool   `json:"enableGridCharge"`
	EnableSell       *bool  `json:"enableSell,omitempty"`
}

type TOUConfigResponse struct {
	BaseResponse
	TouAction           string        `json:"touAction"`
	TimeUseSettingItems []TimeUseItem `json:"timeUseSettingItems"`
}

// ConfigTOU — POST /v1.0/config/tou.
func (c *Client) ConfigTOU(ctx context.Context, deviceSn string) (*TOUConfigResponse, error) {
	var out TOUConfigResponse
	return &out, c.post(ctx, "/v1.0/config/tou", DeviceSnRequest{DeviceSn: deviceSn}, &out)
}

// ─── Control (команды инвертору) ─────────────────────────────────────────────
//
// Каждая команда возвращает orderId. Результат выполнения — GetOrder(orderId):
// status 666 = выполнено, 500 = ошибка.

type ControlResponse struct {
	BaseResponse
	OrderID          int64   `json:"orderId"`
	CollectionTime   FlexInt `json:"collectionTime"`
	ConnectionStatus int     `json:"connectionStatus"` // 0 offline, 1 online
}

// BatteryModeRequest — batteryModeType: GRID_CHARGE | GEN_CHARGE; action: on | off.
type BatteryModeRequest struct {
	DeviceSn        string `json:"deviceSn"`
	BatteryModeType string `json:"batteryModeType"`
	Action          string `json:"action"`
}

// BatteryParameterRequest — paramterType (так в API!): MAX_CHARGE_CURRENT | MAX_DISCHARGE_CURRENT | GRID_CHARGE_AMPERE | BATT_LOW.
type BatteryParameterRequest struct {
	DeviceSn     string `json:"deviceSn"`
	ParamterType string `json:"paramterType"`
	Value        int    `json:"value"`
}

// BatteryTypeRequest — batteryType: BATT_V | BATT_SOC | LI | NO_BATTERY.
type BatteryTypeRequest struct {
	DeviceSn    string `json:"deviceSn"`
	BatteryType string `json:"batteryType"`
}

// CustomControlRequest — произвольная Modbus-команда (hex), timeoutSeconds 10..600.
type CustomControlRequest struct {
	DeviceSn       string `json:"deviceSn"`
	Content        string `json:"content"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// PeakShavingRequest — action: on | off; power — лимит мощности сети.
type PeakShavingRequest struct {
	DeviceSn string `json:"deviceSn"`
	Action   string `json:"action"`
	Power    *int   `json:"power,omitempty"`
}

// SmartLoadRequest — пары onVoltage/offVoltage или onSOC/offSOC.
type SmartLoadRequest struct {
	DeviceSn       string `json:"deviceSn"`
	DeviceType     string `json:"deviceType,omitempty"`
	OnSOC          *int   `json:"onSOC,omitempty"`
	OffSOC         *int   `json:"offSOC,omitempty"`
	OnVoltage      *int   `json:"onVoltage,omitempty"`
	OffVoltage     *int   `json:"offVoltage,omitempty"`
	OnGridAlwaysOn *bool  `json:"onGridAlwaysOn,omitempty"`
}

// EnergyPatternRequest — energyPattern: BATTERY_FIRST | LOAD_FIRST.
type EnergyPatternRequest struct {
	DeviceSn      string `json:"deviceSn"`
	EnergyPattern string `json:"energyPattern"`
}

// LimitControlRequest — (только Micro ESS) SELL_FIRST | ZERO_EXPORT_TO_UPS_LOAD | ZERO_EXPORT_TO_CT | ZERO_EXPORT_TO_WIRELESS_CT.
type LimitControlRequest struct {
	DeviceSn                 string `json:"deviceSn"`
	LimitControlFunctionType string `json:"limitControlFunctionType"`
}

// PowerRequest — powerType: MAX_SELL_POWER | MAX_SOLAR_POWER | ZERO_EXPORT_POWER.
type PowerRequest struct {
	DeviceSn  string `json:"deviceSn"`
	PowerType string `json:"powerType"`
	Value     int    `json:"value"`
}

// ActionRequest — action: on | off (solar sell).
type ActionRequest struct {
	DeviceSn string `json:"deviceSn"`
	Action   string `json:"action"`
}

// TOUSwitchRequest — action on/off; days: MONDAY..SUNDAY (пусто = все дни).
type TOUSwitchRequest struct {
	DeviceSn string   `json:"deviceSn"`
	Action   string   `json:"action"`
	Days     []string `json:"days,omitempty"`
}

// TOUSetRequest — ровно 6 интервалов по порядку.
type TOUSetRequest struct {
	DeviceSn            string        `json:"deviceSn"`
	TimeUseSettingItems []TimeUseItem `json:"timeUseSettingItems"`
}

// WorkModeRequest — workMode: SELLING_FIRST | ZERO_EXPORT_TO_LOAD | ZERO_EXPORT_TO_CT
// (Micro storage: GREEN_POWER_MODE | FULL_CHARGE_MODE | CUSTOMIZED_MODE).
type WorkModeRequest struct {
	DeviceSn string `json:"deviceSn"`
	WorkMode string `json:"workMode"`
}

// DynamicControlRequest — несколько настроек одной командой; не переданные не меняются.
type DynamicControlRequest struct {
	DeviceSn            string        `json:"deviceSn"`
	WorkMode            string        `json:"workMode,omitempty"`
	SolarSellAction     string        `json:"solarSellAction,omitempty"`
	GridChargeAction    string        `json:"gridChargeAction,omitempty"`
	GridChargeAmpere    *int          `json:"gridChargeAmpere,omitempty"`
	MaxSellPower        *int          `json:"maxSellPower,omitempty"`
	MaxSolarPower       *int          `json:"maxSolarPower,omitempty"`
	ZeroExportPower     *int          `json:"zeroExportPower,omitempty"`
	TouAction           string        `json:"touAction,omitempty"`
	TouDays             []string      `json:"touDays,omitempty"`
	TimeUseSettingItems []TimeUseItem `json:"timeUseSettingItems,omitempty"`
}

func (c *Client) control(ctx context.Context, path string, req any) (*ControlResponse, error) {
	var out ControlResponse
	return &out, c.post(ctx, path, req, &out)
}

// BatteryModeControl — POST /v1.0/order/battery/modeControl.
func (c *Client) BatteryModeControl(ctx context.Context, r BatteryModeRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/battery/modeControl", r)
}

// BatteryParameterUpdate — POST /v1.0/order/battery/parameter/update.
func (c *Client) BatteryParameterUpdate(ctx context.Context, r BatteryParameterRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/battery/parameter/update", r)
}

// BatteryTypeUpdate — POST /v1.0/order/battery/type/update.
func (c *Client) BatteryTypeUpdate(ctx context.Context, r BatteryTypeRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/battery/type/update", r)
}

// CustomControl — POST /v1.0/order/customControl (Modbus-команда).
func (c *Client) CustomControl(ctx context.Context, r CustomControlRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/customControl", r)
}

// GridPeakShavingControl — POST /v1.0/order/gridPeakShaving/control.
func (c *Client) GridPeakShavingControl(ctx context.Context, r PeakShavingRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/gridPeakShaving/control", r)
}

// SmartLoadUpdate — POST /v1.0/order/smartload/update.
func (c *Client) SmartLoadUpdate(ctx context.Context, r SmartLoadRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/smartload/update", r)
}

// EnergyPatternUpdate — POST /v1.0/order/sys/energyPattern/update.
func (c *Client) EnergyPatternUpdate(ctx context.Context, r EnergyPatternRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/energyPattern/update", r)
}

// LimitControl — POST /v1.0/order/sys/limitControl.
func (c *Client) LimitControl(ctx context.Context, r LimitControlRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/limitControl", r)
}

// PowerUpdate — POST /v1.0/order/sys/power/update.
func (c *Client) PowerUpdate(ctx context.Context, r PowerRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/power/update", r)
}

// SolarSellControl — POST /v1.0/order/sys/solarSell/control.
func (c *Client) SolarSellControl(ctx context.Context, r ActionRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/solarSell/control", r)
}

// TOUSwitch — POST /v1.0/order/sys/tou/switch.
func (c *Client) TOUSwitch(ctx context.Context, r TOUSwitchRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/tou/switch", r)
}

// TOUUpdate — POST /v1.0/order/sys/tou/update.
func (c *Client) TOUUpdate(ctx context.Context, r TOUSetRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/tou/update", r)
}

// WorkModeUpdate — POST /v1.0/order/sys/workMode/update.
func (c *Client) WorkModeUpdate(ctx context.Context, r WorkModeRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/order/sys/workMode/update", r)
}

// DynamicControl — POST /v1.0/strategy/dynamicControl.
func (c *Client) DynamicControl(ctx context.Context, r DynamicControlRequest) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/strategy/dynamicControl", r)
}

// DynamicControlRead — POST /v1.0/strategy/dynamicControl/read: отправить команду чтения настроек.
func (c *Client) DynamicControlRead(ctx context.Context, deviceSn string) (*ControlResponse, error) {
	return c.control(ctx, "/v1.0/strategy/dynamicControl/read", DeviceSnRequest{DeviceSn: deviceSn})
}

type DynamicConfigResponse struct {
	BaseResponse
	WorkMode            string        `json:"workMode"`
	SolarSellAction     string        `json:"solarSellAction"`
	GridChargeAction    string        `json:"gridChargeAction"`
	GridChargeAmpere    *int          `json:"gridChargeAmpere"`
	MaxChargeCurrent    *int          `json:"maxChargeCurrent"`
	MaxDischargeCurrent *int          `json:"maxDischargeCurrent"`
	MaxSellPower        *int          `json:"maxSellPower"`
	MaxSolarPower       *int          `json:"maxSolarPower"`
	ZeroExportPower     *int          `json:"zeroExportPower"`
	TouAction           string        `json:"touAction"`
	TouDays             []string      `json:"touDays"`
	TimeUseSettingItems []TimeUseItem `json:"timeUseSettingItems"`
}

// DynamicControlReadResult — POST /v1.0/strategy/dynamicControl/readResult.
func (c *Client) DynamicControlReadResult(ctx context.Context, orderID int64) (*DynamicConfigResponse, error) {
	var out DynamicConfigResponse
	return &out, c.post(ctx, "/v1.0/strategy/dynamicControl/readResult", map[string]any{"orderId": orderID}, &out)
}

// Статусы команды.
const (
	OrderToBeSent   = 0
	OrderSending    = 100
	OrderUpgrading  = 300
	OrderTerminated = 400
	OrderFailed     = 500
	OrderSucceeded  = 666
)

type OrderResponse struct {
	BaseResponse
	OrderID        int64      `json:"orderId"`
	Status         int        `json:"status"`
	Error          string     `json:"error"`
	OrderResult    string     `json:"orderResult"`
	AnalysisResult string     `json:"analysisResult"`
	CreateBy       *int64     `json:"createBy"`
	CreateTime     FlexString `json:"createTime"`
	UpdateTime     FlexString `json:"updateTime"`
	DeviceTimezone string     `json:"deviceTimezone"`
}

// GetOrder — GET /v1.0/order/{orderId}: результат выполнения команды.
func (c *Client) GetOrder(ctx context.Context, orderID int64) (*OrderResponse, error) {
	var out OrderResponse
	return &out, c.call(ctx, http.MethodGet, fmt.Sprintf("/v1.0/order/%d", orderID), nil, &out)
}
