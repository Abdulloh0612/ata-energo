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
	"strings"
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

// Session material must never be serialized in API responses or diagnostics.
func (s Session) MarshalJSON() ([]byte, error) { return []byte(`{}`), nil }

func (e *APIError) sessionExpired() bool {
	return e.HTTPStatus == http.StatusUnauthorized ||
		(e.HTTPStatus >= 200 && e.HTTPStatus < 300 && (e.Code == 4 || e.Code == 35 || e.Code == 303))
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
	return &Client{base: strings.TrimRight(base, "/"), credentials: credentials, http: &hc}
}

// request is called under mu: response Auth rotations and login cannot race.
func (c *Client) request(ctx context.Context, method, path string, query url.Values, body any, authenticated bool, out any) error {
	parsed, err := url.Parse(path)
	if err != nil || !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") ||
		parsed.IsAbs() || parsed.Host != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("ValueClouds: некорректный путь запроса")
	}
	var data []byte
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
	// Authentication failures may be returned as an HTML gateway response.
	// Classify the HTTP status before attempting to decode a JSON envelope.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{Code: -1, HTTPStatus: resp.StatusCode}
	}
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
		parsed, parseErr := strconv.Atoi(envelope.Code.String())
		if parseErr != nil {
			return errors.New("ValueClouds: некорректный код ответа API")
		}
		code = parsed
	}
	// Both web and app envelopes use 0/200. An explicit failure always wins.
	if (envelope.Success != nil && !*envelope.Success) ||
		(envelope.Code != nil && code != 0 && code != 200) ||
		(envelope.Code == nil && (envelope.Success == nil || !*envelope.Success)) {
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
		Token  string `json:"token"`
		Secret string `json:"secret"`
		Auth   string `json:"auth"`
		UserID Value  `json:"userId"`
		UID    Value  `json:"uid"`
	}
	err := c.request(ctx, http.MethodPost, loginPath, nil, map[string]string{"account": c.credentials.Account, "password": c.credentials.PasswordSHA1, "project": "IOT"}, false, &data)
	if err != nil {
		return err
	}
	if data.Token == "" || data.Secret == "" {
		return errors.New("ValueClouds: вход не вернул token и secret")
	}
	c.session.UserID = string(data.UserID)
	if c.session.UserID == "" {
		c.session.UserID = string(data.UID)
	}
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
	if errors.As(err, &apiErr) && apiErr.sessionExpired() {
		c.session = Session{}
		// Do not replay POSTs or legacy GETs which can change device settings.
		if !readOnlyRequest(method, path) {
			return err
		}
		if err = c.login(ctx); err != nil {
			return err
		}
		err = c.request(ctx, method, path, query, body, true, out)
		if errors.As(err, &apiErr) && apiErr.sessionExpired() {
			c.session = Session{}
		}
		return err
	}
	return err
}

func readOnlyRequest(method, path string) bool {
	if method == http.MethodPost {
		return path == "/dgm/api/dgm/itemseybond/getInfoByParams"
	}
	if method != http.MethodGet {
		return false
	}
	switch path {
	case devicesPath,
		"/ppr/web/iotSysUser/auth/getInfo",
		"/ppe/api/auth/web/querySPDeviceLastData",
		"/ppe/api/auth/web/queryDeviceEnergyFlow",
		"/ppe/api/auth/web/queryDevicePars",
		"/ppe/api/auth/web/queryDeviceChartsFields",
		"/dev/api/auth/valueCloud/dev/singleDeviceInfo",
		"/alm/api/auth/web/device/warnings",
		"/dcs/api/auth/web/ess/device/outputPowerOneDay",
		"/dcs/api/auth/web/ess/device/energyMonthPerDay",
		"/dcs/api/auth/web/ess/device/energyYearPerMonth":
		return true
	}
	return false
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
