// Package valueclouds implements the ValueClouds web API.
package valueclouds

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

const BaseURL = "https://api.valueclouds.com"
const loginPath = "/ppr/web/login/login"

type Credentials struct{ Account, PasswordSHA1 string }
type Session struct{ Token, Secret, Auth, UserID string }
type Client struct {
	base        string
	credentials Credentials
	http        *http.Client
	mu          sync.Mutex
	session     Session
}

type APIError struct {
	Code       int
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ValueClouds: ошибка API (код %d, HTTP %d)", e.Code, e.HTTPStatus)
}
func HashPassword(password string) string {
	sum := sha1.Sum([]byte(password))
	return hex.EncodeToString(sum[:])
}
func Sign(path, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	_, _ = h.Write([]byte(path))
	return hex.EncodeToString(h.Sum(nil))
}
func NewClient(base string, credentials Credentials, transport *http.Client) *Client {
	if base == "" {
		base = BaseURL
	}
	if transport == nil {
		transport = &http.Client{Timeout: 30 * time.Second}
	}
	// Never forward credentials across redirects, even to another official host.
	hc := *transport
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: base, credentials: credentials, http: &hc}
}

// request is called under mu: response Auth rotations and login cannot race.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body any, authenticated bool, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(data))
	if err != nil {
		return errors.New("ValueClouds: некорректный запрос")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("project", "IOT")
	req.Header.Set("i18n", "ru_RU")
	req.Header.Set("Origin", "https://www.valueclouds.com")
	req.Header.Set("Referer", "https://www.valueclouds.com/")
	if authenticated {
		req.Header.Set("token", c.session.Token)
		req.Header.Set("sign", Sign(path, c.session.Secret))
		if c.session.Auth != "" {
			req.Header.Set("Auth", c.session.Auth)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("ValueClouds: ошибка соединения")
	}
	defer resp.Body.Close()
	var envelope struct {
		Code    *json.Number    `json:"code"`
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20+1))
	if err != nil || len(raw) > 8<<20 {
		return errors.New("ValueClouds: не удалось прочитать ответ")
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return errors.New("ValueClouds: некорректный ответ API")
	}
	code := -1
	if envelope.Code != nil {
		code, _ = strconv.Atoi(envelope.Code.String())
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || code != 0 || (envelope.Success != nil && !*envelope.Success) {
		return &APIError{code, resp.StatusCode}
	}
	if auth := resp.Header.Get("Auth"); auth != "" {
		c.session.Auth = auth
	}
	if out != nil {
		if err = json.Unmarshal(envelope.Data, out); err != nil {
			return errors.New("ValueClouds: неожиданный формат данных")
		}
	}
	return nil
}
func (c *Client) login(ctx context.Context) error {
	if c.credentials.Account == "" || c.credentials.PasswordSHA1 == "" {
		return errors.New("ValueClouds: укажите аккаунт и пароль")
	}
	c.session = Session{}
	var data struct {
		Token  string      `json:"token"`
		Secret string      `json:"secret"`
		Auth   string      `json:"auth"`
		UserID json.Number `json:"userId"`
	}
	err := c.request(ctx, http.MethodPost, loginPath, nil, map[string]string{"account": c.credentials.Account, "password": c.credentials.PasswordSHA1, "project": "IOT"}, false, &data)
	if err != nil {
		return err
	}
	if data.Token == "" || data.Secret == "" {
		return errors.New("ValueClouds: вход не вернул token и secret")
	}
	c.session.UserID = data.UserID.String()
	c.session.Token = data.Token
	c.session.Secret = data.Secret
	if data.Auth != "" {
		c.session.Auth = data.Auth
	}
	return nil
}
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session.Token == "" {
		if err := c.login(ctx); err != nil {
			return err
		}
	}
	err := c.request(ctx, method, path, query, body, true, out)
	var apiErr *APIError
	if errors.As(err, &apiErr) && (apiErr.Code == 4 || apiErr.Code == 35 || apiErr.Code == 303 || apiErr.HTTPStatus == 401) {
		if err = c.login(ctx); err != nil {
			return err
		}
		return c.request(ctx, method, path, query, body, true, out)
	}
	return err
}
func (c *Client) Test(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.login(ctx); err != nil {
		return err
	}
	// Never return the user profile: it includes the signing secret.
	return c.request(ctx, http.MethodGet, "/ppr/web/iotSysUser/auth/getInfo", nil, nil, true, nil)
}

const devicesPath = "/dev/api/auth/valueCloud/dev/listTDeviceInfo"

type Device struct {
	ID        string          `json:"id"`
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
			Total *int     `json:"total"`
			Items []Device `json:"items"`
		}
		if err := c.Do(ctx, http.MethodGet, devicesPath, url.Values{"page": {strconv.Itoa(page)}, "pageSize": {"100"}}, nil, &data); err != nil {
			return nil, err
		}
		for _, d := range data.Items {
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
func (c *Client) EnergyFlow(ctx context.Context, d Device) (map[string]any, error) {
	var data map[string]any
	err := c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/queryDeviceEnergyFlow", d.Query(), nil, &data)
	return data, err
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
func (c *Client) Latest(ctx context.Context, d Device) (map[string]any, error) {
	var data map[string]any
	err := c.Do(ctx, http.MethodGet, "/ppe/api/auth/web/querySPDeviceLastData", d.Query(), nil, &data)
	return data, err
}
