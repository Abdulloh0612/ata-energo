package deye

import (
	"context"
	"sort"
	"strings"
)

// ─── Device ──────────────────────────────────────────────────────────────────

// Состояние устройства в /device/latest и /device/list.
const (
	DeviceStateOnline  = 1
	DeviceStateAlert   = 2
	DeviceStateOffline = 3
)

type DataPoint struct {
	Key   string     `json:"key"`
	Value FlexString `json:"value"`
	Unit  string     `json:"unit"`
}

type DeviceLatestData struct {
	DeviceSn       string      `json:"deviceSn"`
	DeviceType     string      `json:"deviceType"`
	DeviceState    int         `json:"deviceState"` // 1 online, 2 alert, 3 offline
	CollectionTime FlexInt     `json:"collectionTime"`
	DataList       []DataPoint `json:"dataList"`
}

type DeviceLatestRequest struct {
	DeviceList []string `json:"deviceList"` // до 10 SN за раз
	DeviceType string   `json:"deviceType,omitempty"`
}

type DeviceLatestResponse struct {
	BaseResponse
	DeviceDataList []DeviceLatestData `json:"deviceDataList"`
}

// MaxLatestBatch — сколько устройств можно запросить в /device/latest за раз.
const MaxLatestBatch = 10

// DeviceLatest — POST /v1.0/device/latest: последние показания (до 10 устройств за раз).
func (c *Client) DeviceLatest(ctx context.Context, req DeviceLatestRequest) (*DeviceLatestResponse, error) {
	var out DeviceLatestResponse
	return &out, c.post(ctx, "/v1.0/device/latest", req, &out)
}

type DeviceDetailItem struct {
	DeviceSn    string  `json:"deviceSn"`
	DeviceID    int64   `json:"deviceId"`
	DeviceType  string  `json:"deviceType"`
	DeviceState int     `json:"deviceState"`
	ProductID   string  `json:"productId"`
	StationID   int64   `json:"stationId"`
	UpdateTime  FlexInt `json:"updateTime"`
}

type DeviceListResponse struct {
	BaseResponse
	Total      int                `json:"total"`
	DeviceList []DeviceDetailItem `json:"deviceList"`
}

// DeviceList — POST /v1.0/device/list: все устройства бизнес-аккаунта (size до 200).
func (c *Client) DeviceList(ctx context.Context, req PageRequest) (*DeviceListResponse, error) {
	var out DeviceListResponse
	return &out, c.post(ctx, "/v1.0/device/list", req, &out)
}

type DeviceSnRequest struct {
	DeviceSn   string `json:"deviceSn"`
	DeviceType string `json:"deviceType,omitempty"`
}

type MeasurePointsResponse struct {
	BaseResponse
	DeviceSn      string   `json:"deviceSn"`
	DeviceType    string   `json:"deviceType"`
	ProductID     string   `json:"productId"`
	StationID     int64    `json:"stationId"`
	TimeZone      string   `json:"timeZone"`
	MeasurePoints []string `json:"measurePoints"`
}

// DeviceMeasurePoints — POST /v1.0/device/measurePoints: какие точки измерения есть у устройства.
func (c *Client) DeviceMeasurePoints(ctx context.Context, req DeviceSnRequest) (*MeasurePointsResponse, error) {
	var out MeasurePointsResponse
	return &out, c.post(ctx, "/v1.0/device/measurePoints", req, &out)
}

// DeviceHistoryRequest — granularity: 1 — точки за день startAt (yyyy-MM-dd, нужны measurePoints),
// 2 — по дням (yyyy-MM-dd, до 31 дня), 3 — по месяцам (yyyy-MM, до 12), 4 — по годам (yyyy).
type DeviceHistoryRequest struct {
	DeviceSn      string   `json:"deviceSn"`
	DeviceType    string   `json:"deviceType,omitempty"`
	Granularity   int      `json:"granularity"`
	StartAt       string   `json:"startAt"`
	EndAt         string   `json:"endAt,omitempty"`
	MeasurePoints []string `json:"measurePoints,omitempty"`
}

type DeviceHistoryItem struct {
	Time     FlexString       `json:"time"`
	ItemList []map[string]any `json:"itemList"`
	Extra    map[string]any   `json:"-"`
}

type DeviceHistoryResponse struct {
	BaseResponse
	DeviceSn    string              `json:"deviceSn"`
	DeviceID    int64               `json:"deviceId"`
	DeviceType  string              `json:"deviceType"`
	Granularity int                 `json:"granularity"`
	DataList    []DeviceHistoryItem `json:"dataList"`
}

// DeviceHistory — POST /v1.0/device/history.
func (c *Client) DeviceHistory(ctx context.Context, req DeviceHistoryRequest) (*DeviceHistoryResponse, error) {
	var out DeviceHistoryResponse
	return &out, c.post(ctx, "/v1.0/device/history", req, &out)
}

// DeviceHistoryRawRequest — unix-секунды, диапазон до 5 дней.
type DeviceHistoryRawRequest struct {
	DeviceSn       string   `json:"deviceSn"`
	DeviceType     string   `json:"deviceType,omitempty"`
	StartTimestamp int64    `json:"startTimestamp"`
	EndTimestamp   int64    `json:"endTimestamp"`
	MeasurePoints  []string `json:"measurePoints"`
}

// DeviceHistoryRaw — POST /v1.0/device/historyRaw.
func (c *Client) DeviceHistoryRaw(ctx context.Context, req DeviceHistoryRawRequest) (*DeviceHistoryResponse, error) {
	var out DeviceHistoryResponse
	return &out, c.post(ctx, "/v1.0/device/historyRaw", req, &out)
}

// DeviceHistoryRawBatched respects Deye's five-measurement limit and joins
// batches by timestamp; missing samples remain missing rather than becoming zero.
func (c *Client) DeviceHistoryRawBatched(ctx context.Context, req DeviceHistoryRawRequest) (*DeviceHistoryResponse, error) {
	if len(req.MeasurePoints) <= 5 {
		return c.DeviceHistoryRaw(ctx, req)
	}
	var result *DeviceHistoryResponse
	rows := make(map[FlexString]*DeviceHistoryItem)
	for offset := 0; offset < len(req.MeasurePoints); offset += 5 {
		batch := req
		batch.MeasurePoints = req.MeasurePoints[offset:min(offset+5, len(req.MeasurePoints))]
		reply, err := c.DeviceHistoryRaw(ctx, batch)
		if err != nil {
			return nil, err
		}
		if result == nil {
			result = reply
		}
		for _, item := range reply.DataList {
			row := rows[item.Time]
			if row == nil {
				row = &DeviceHistoryItem{Time: item.Time}
				rows[item.Time] = row
			}
			row.ItemList = append(row.ItemList, item.ItemList...)
		}
	}
	result.DataList = make([]DeviceHistoryItem, 0, len(rows))
	for _, row := range rows {
		result.DataList = append(result.DataList, *row)
	}
	sort.Slice(result.DataList, func(i, j int) bool { return result.DataList[i].Time < result.DataList[j].Time })
	return result, nil
}

// DeviceAlertListRequest — unix-секунды, диапазон не больше 30 дней.
type DeviceAlertListRequest struct {
	DeviceSn       string `json:"deviceSn"`
	DeviceType     string `json:"deviceType,omitempty"`
	StartTimestamp int64  `json:"startTimestamp"`
	EndTimestamp   int64  `json:"endTimestamp"`
	Page           int    `json:"page,omitempty"`
	Size           int    `json:"size,omitempty"` // max 100
}

type DeviceAlertListResponse struct {
	BaseResponse
	DeviceSn   string      `json:"deviceSn"`
	DeviceID   int64       `json:"deviceId"`
	DeviceType string      `json:"deviceType"`
	Total      int         `json:"total"`
	AlertList  []AlertItem `json:"alertList"`
}

// DeviceAlertList — POST /v1.0/device/alertList.
func (c *Client) DeviceAlertList(ctx context.Context, req DeviceAlertListRequest) (*DeviceAlertListResponse, error) {
	var out DeviceAlertListResponse
	return &out, c.post(ctx, "/v1.0/device/alertList", req, &out)
}

type DeviceRegisterRequest struct {
	DeviceSn  string `json:"deviceSn"`
	GatewaySn string `json:"gatewaySn"`
	StationID int64  `json:"stationId"`
}

type DeviceRegisterResponse struct {
	BaseResponse
	GatewayID int64 `json:"gatewayId"`
}

// DeviceRegister — POST /v1.0/device/register: добавить логгер в станцию.
func (c *Client) DeviceRegister(ctx context.Context, req DeviceRegisterRequest) (*DeviceRegisterResponse, error) {
	var out DeviceRegisterResponse
	return &out, c.post(ctx, "/v1.0/device/register", req, &out)
}

type LoggerRequest struct {
	DeviceSns []string `json:"deviceSns"` // до 10 за раз
}

// AddLoggers — POST /v1.0/device/addLogger (только бизнес-аккаунт).
func (c *Client) AddLoggers(ctx context.Context, sns []string) (*BaseResponse, error) {
	var out BaseResponse
	return &out, c.post(ctx, "/v1.0/device/addLogger", LoggerRequest{DeviceSns: sns}, &out)
}

// DeleteLoggers — POST /v1.0/device/deleteLogger (только бизнес-аккаунт).
func (c *Client) DeleteLoggers(ctx context.Context, sns []string) (*BaseResponse, error) {
	var out BaseResponse
	return &out, c.post(ctx, "/v1.0/device/deleteLogger", LoggerRequest{DeviceSns: sns}, &out)
}

// ─── Удобства для dataList ───────────────────────────────────────────────────

// NormKey — ключ точки в нормальном виде: нижний регистр, только буквы и цифры ("Battery SOC" -> "batterysoc").
func NormKey(k string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(k) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Points — dataList в виде карты norm(key) -> значение (кВт/кВт*ч уже переведены в Вт/Вт*ч).
type Points map[string]float64

// ToPoints превращает dataList в Points. Нечисловые значения пропускаются.
func ToPoints(list []DataPoint) Points {
	p := Points{}
	for _, d := range list {
		v, ok := d.Value.Float()
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(d.Unit)) {
		case "kw", "kva", "kvar":
			v *= 1000
		case "mw":
			v *= 1_000_000
		}
		p[NormKey(d.Key)] = v
	}
	return p
}

// First — первое найденное значение из списка ключей-кандидатов (ключи в виде NormKey).
func (p Points) First(keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := p[k]; ok {
			return v, true
		}
	}
	return 0, false
}

// Sum — сумма всех найденных ключей (ok=false, если не найдено ни одного).
func (p Points) Sum(keys ...string) (float64, bool) {
	var s float64
	found := false
	for _, k := range keys {
		if v, ok := p[k]; ok {
			s += v
			found = true
		}
	}
	return s, found
}
