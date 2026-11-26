package valueclouds

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// Value accepts numeric and textual measurements without losing zero or gaps.
type Value string

func (v *Value) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*v = ""
		return nil
	}
	var text string
	if json.Unmarshal(data, &text) == nil {
		*v = Value(text)
		return nil
	}
	var n json.Number
	if json.Unmarshal(data, &n) == nil {
		*v = Value(n.String())
		return nil
	}
	return errors.New("ValueClouds: некорректное значение измерения")
}

type Measurement struct {
	ID     string `json:"id"`
	Name   string `json:"par"`
	Value  Value  `json:"val"`
	Unit   string `json:"unit"`
	Charts bool   `json:"charts"`
}
type LastData struct {
	Timestamp int64                    `json:"gts"`
	Groups    map[string][]Measurement `json:"pars"`
}

func (d *LastData) UnmarshalJSON(raw []byte) error {
	var wire struct {
		Timestamp Value                    `json:"gts"`
		Groups    map[string][]Measurement `json:"pars"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	var timestamp int64
	if wire.Timestamp != "" {
		var err error
		timestamp, err = strconv.ParseInt(string(wire.Timestamp), 10, 64)
		if err != nil {
			return errors.New("ValueClouds: некорректное время измерения")
		}
	}
	d.Timestamp, d.Groups = timestamp, wire.Groups
	return nil
}

type FlowPoint struct {
	Parameter string `json:"par"`
	Value     Value  `json:"val"`
	Unit      string `json:"unit"`
	Status    int    `json:"status"`
}
type EnergyFlow struct {
	Status    int         `json:"status"`
	Timestamp int64       `json:"date"`
	Battery   []FlowPoint `json:"bt_status"`
	Solar     []FlowPoint `json:"pv_status"`
	Grid      []FlowPoint `json:"gd_status"`
	Load      []FlowPoint `json:"bc_status"`
	Generator []FlowPoint `json:"ol_status"`
}
type ActiveParameter struct {
	Updated   string `json:"date"`
	Name      string `json:"name"`
	Parameter string `json:"par"`
	Order     int    `json:"sortnum"`
	Unit      string `json:"unit"`
	Value     Value  `json:"val"`
}
type ChartField struct {
	Name      string `json:"name"`
	Parameter string `json:"optional"`
	Order     int    `json:"order"`
	Unit      string `json:"uint"`
	Visible   bool   `json:"visable"`
}
type LoggerInfo struct {
	TypeName string `json:"typeName"`
	Firmware string `json:"firmwareVersion"`
}
type DeviceInfo struct {
	Device
	Firmware  string      `json:"firmwareVersion"`
	TypeName  string      `json:"typeName"`
	Timezone  int         `json:"timezone"`
	Collector *LoggerInfo `json:"collectorDTO"`
}

func (c *Client) DeviceEnergyFlow(ctx context.Context, d Device) (*EnergyFlow, error) {
	var result EnergyFlow
	err := c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/queryDeviceEnergyFlow", d.Query(), nil, &result)
	return &result, err
}
func (c *Client) DeviceLastData(ctx context.Context, d Device) (*LastData, error) {
	var result LastData
	err := c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/querySPDeviceLastData", d.Query(), nil, &result)
	return &result, err
}
func (c *Client) userID(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session.Token == "" {
		if err := c.login(ctx); err != nil {
			return "", err
		}
	}
	if c.session.UserID == "" {
		return "", errors.New("ValueClouds: вход не вернул ID пользователя")
	}
	return c.session.UserID, nil
}
func (c *Client) DeviceParameters(ctx context.Context, d Device) ([]ActiveParameter, error) {
	uid := string(d.UID)
	var err error
	if uid == "" {
		uid, err = c.userID(ctx)
		if err != nil {
			return nil, err
		}
	}
	query := d.Query()
	query.Set("uid", uid)
	result := []ActiveParameter{}
	err = c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/queryDevicePars", query, nil, &result)
	return result, err
}
func (c *Client) DeviceInfo(ctx context.Context, d Device) (*DeviceInfo, error) {
	var result DeviceInfo
	query := d.Query()
	query.Del("i18n")
	err := c.Do(ctx, http.MethodGet, "/dev/api/auth/valueCloud/dev/singleDeviceInfo", query, nil, &result)
	return &result, err
}
func (c *Client) DeviceChartFields(ctx context.Context, d Device) ([]ChartField, error) {
	uid, err := c.userID(ctx)
	if err != nil {
		return nil, err
	}
	result := []ChartField{}
	err = c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/queryDeviceChartsFields", url.Values{"uid": {uid}, "devcode": {strconv.Itoa(d.Code)}, "i18n": {"ru_RU"}}, nil, &result)
	return result, err
}

func (c *Client) EnergyFlow(ctx context.Context, d Device) (map[string]any, error) {
	var data map[string]any
	err := c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/queryDeviceEnergyFlow", d.Query(), nil, &data)
	return data, err
}

func (c *Client) Latest(ctx context.Context, d Device) (map[string]any, error) {
	var data map[string]any
	err := c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/querySPDeviceLastData", d.Query(), nil, &data)
	return data, err
}
