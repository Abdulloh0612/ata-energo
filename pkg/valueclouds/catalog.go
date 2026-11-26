package valueclouds

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

const devicesPath = "/dev/api/auth/valueCloud/dev/listTDeviceInfo"

type Device struct {
	ID        Value           `json:"id"`
	Name      string          `json:"deviceName"`
	PN        string          `json:"pn"`
	SN        string          `json:"sn"`
	Serial    string          `json:"deviceSn"`
	Code      int             `json:"devcode"`
	Address   int             `json:"devaddr"`
	Status    int             `json:"deviceStatus"`
	PID       json.RawMessage `json:"pid"`
	GroupName string          `json:"deviceGroupName"`
	Updated   string          `json:"ltsTimeStr"`
	Created   string          `json:"createTimeStr"`
	UID       Value           `json:"userId"`
}

func (d Device) Key() string {
	return url.QueryEscape(d.PN) + ":" + url.QueryEscape(d.SN) + ":" + strconv.Itoa(d.Address)
}
func (d Device) Query() url.Values {
	return url.Values{"pn": {d.PN}, "sn": {d.SN}, "devcode": {strconv.Itoa(d.Code)}, "devaddr": {strconv.Itoa(d.Address)}, "i18n": {"ru_RU"}}
}
func (c *Client) Devices(ctx context.Context) ([]Device, error) {
	out := make([]Device, 0)
	seen := map[string]bool{}
	for page := 1; page <= 500; page++ {
		var data struct {
			Total   *int              `json:"total"`
			Items   []json.RawMessage `json:"items"`
			Devices []json.RawMessage `json:"device"`
		}
		if err := c.Do(ctx, http.MethodGet, devicesPath, url.Values{"page": {strconv.Itoa(page)}, "pageSize": {"100"}}, nil, &data); err != nil {
			return nil, err
		}
		if data.Items == nil {
			data.Items = data.Devices
		}
		if data.Items == nil {
			return nil, errors.New("ValueClouds: нет списка устройств в ответе")
		}
		for _, raw := range data.Items {
			d, err := decodeDevice(raw)
			if err != nil {
				return nil, err
			}
			if d.PN == "" || d.SN == "" || d.Code <= 0 || d.Address < 0 || seen[d.Key()] {
				return nil, errors.New("ValueClouds: некорректный или повторяющийся прибор")
			}
			seen[d.Key()] = true
			out = append(out, d)
		}
		if data.Total != nil && len(out) == *data.Total {
			return out, nil
		}
		if data.Total == nil && len(data.Items) < 100 {
			return out, nil
		}
		if len(data.Items) == 0 || (data.Total != nil && len(out) > *data.Total) {
			return nil, errors.New("ValueClouds: неполный каталог устройств")
		}
	}
	return nil, errors.New("ValueClouds: превышен предел страниц")
}

func decodeDevice(raw json.RawMessage) (Device, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Device{}, errors.New("ValueClouds: некорректная запись устройства")
	}
	// These fields are numeric in web responses and may be strings in app data.
	for _, key := range []string{"devcode", "devaddr", "deviceStatus"} {
		value, exists := fields[key]
		if !exists || string(value) == "null" {
			if key == "deviceStatus" {
				fields[key] = json.RawMessage(`-1`)
				continue
			}
			return Device{}, errors.New("ValueClouds: неполная идентификация устройства")
		}
		var v Value
		if err := json.Unmarshal(value, &v); err != nil {
			return Device{}, err
		}
		n, err := strconv.Atoi(string(v))
		if err != nil {
			return Device{}, errors.New("ValueClouds: некорректное числовое поле устройства")
		}
		fields[key] = json.RawMessage(strconv.Itoa(n))
	}
	encoded, _ := json.Marshal(fields)
	var d Device
	if err := json.Unmarshal(encoded, &d); err != nil {
		return Device{}, errors.New("ValueClouds: некорректная запись устройства")
	}
	if d.SN == "" {
		d.SN = d.Serial
	}
	return d, nil
}

// Projects returns complete pages before the local catalog can archive absent rows.
func (c *Client) Projects(ctx context.Context) ([]map[string]any, error) {
	out := []map[string]any{}
	seen := map[string]bool{}
	for page := 1; page <= 500; page++ {
		var data struct {
			Total   *int             `json:"total"`
			Records []map[string]any `json:"records"`
		}
		if err := c.Do(ctx, http.MethodPost, "/dgm/api/dgm/itemseybond/getInfoByParams", url.Values{"current": {strconv.Itoa(page)}, "size": {"100"}}, map[string]any{"factoryFlag": true, "itemType": 28}, &data); err != nil {
			return nil, err
		}
		if data.Records == nil {
			return nil, errors.New("ValueClouds: нет списка проектов в ответе")
		}
		for _, record := range data.Records {
			id := ProjectID(record)
			if id == "" || seen[id] {
				return nil, errors.New("ValueClouds: некорректный или повторяющийся проект")
			}
			seen[id] = true
			out = append(out, record)
		}
		if data.Total != nil && len(out) == *data.Total {
			return out, nil
		}
		if data.Total == nil && len(data.Records) < 100 {
			return out, nil
		}
		if len(data.Records) == 0 || (data.Total != nil && len(out) > *data.Total) {
			return nil, errors.New("ValueClouds: неполный каталог проектов")
		}
	}
	return nil, errors.New("ValueClouds: превышен предел страниц проектов")
}
func ProjectID(record map[string]any) string {
	for _, key := range []string{"id", "pid", "itemId"} {
		if v, ok := record[key].(string); ok && v != "" && v != "0" {
			return v
		}
		if v, ok := record[key].(float64); ok && v > 0 && v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
	}
	return ""
}

// Status values follow ValueClouds: 0 offline, 1 normal, 2 fault,
// 3 standby, 4 warning. Standby and alarms still supply measurements.
func (d Device) Online() bool { return d.Status >= 1 && d.Status <= 4 }
