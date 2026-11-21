// Package deye — клиент Deye Cloud OpenAPI (https://developer.deyecloud.com/api).
//
// Все методы — POST JSON (кроме GET /order/{id}), заголовок "authorization: Bearer <token>".
// Токен берётся через POST /v1.0/account/token?appId=... и кешируется до истечения
// (обычно ~60 дней); при ошибке авторизации клиент один раз запрашивает токен заново.
//
// Регионы (базовые адреса):
//
//	EU: https://eu1-developer.deyecloud.com
//	US: https://us1-developer.deyecloud.com
package deye

import (
	"bytes"
	"context"
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

const (
	BaseURLEU = "https://eu1-developer.deyecloud.com"
	BaseURLUS = "https://us1-developer.deyecloud.com"

	// CodeSuccess — код успешного ответа.
	CodeSuccess = "1000000"
)

// Code — код ответа; Deye присылает его то строкой, то числом.
type Code string

func (c *Code) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "null" {
		s = ""
	}
	*c = Code(s)
	return nil
}

// FlexString — строка, которая в ответе может прийти числом (например, value в dataList).
type FlexString string

func (f *FlexString) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*f = ""
		return nil
	}
	if strings.HasPrefix(s, `"`) {
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		*f = FlexString(v)
		return nil
	}
	*f = FlexString(s)
	return nil
}

// Float — значение как число (ok=false, если это не число).
func (f FlexString) Float() (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(string(f)), 64)
	return v, err == nil
}

// FlexInt — целое, которое может прийти строкой.
type FlexInt int64

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil // нечисловое значение — игнорируем, не роняем весь ответ
	}
	*f = FlexInt(v)
	return nil
}

// BaseResponse — общая "обёртка" всех ответов.
type BaseResponse struct {
	Code      Code   `json:"code"`
	Msg       string `json:"msg"`
	Success   bool   `json:"success"`
	RequestID string `json:"requestId"`
}

func (b *BaseResponse) base() *BaseResponse { return b }

type response interface{ base() *BaseResponse }

// APIError — ошибка, которую вернул Deye Cloud.
type APIError struct {
	HTTPStatus int
	Code       string
	Msg        string
	RequestID  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("deye: code=%s (http %d, requestId=%s)", e.Code, e.HTTPStatus, e.RequestID)
}

// IsAuthError — ошибка токена (нет/просрочен/неверный) — стоит получить токен заново.
func IsAuthError(err error) bool {
	var e *APIError
	if !errors.As(err, &e) {
		return false
	}
	if e.HTTPStatus == http.StatusUnauthorized {
		return true
	}
	msg := strings.ToLower(e.Msg)
	return strings.HasPrefix(e.Code, "2101") || strings.Contains(msg, "token")
}

// Credentials — данные для получения токена. Нужен ОДИН из: Email, Username или Mobile (+CountryCode).
type Credentials struct {
	AppID          string
	AppSecret      string
	Email          string
	Username       string
	Mobile         string
	CountryCode    string
	PasswordSHA256 string // SHA-256 пароля в hex (нижний регистр) — см. HashPassword
	CompanyID      *int64 // для бизнес-аккаунта (организации); пусто — личный аккаунт
}

// HashPassword — Deye требует пароль в виде SHA-256 (hex, нижний регистр).
func HashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// Client — клиент одного аккаунта Deye Cloud. Безопасен для одновременного использования.
type Client struct {
	baseURL string
	creds   Credentials
	http    *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewClient — baseURL пустой = EU.
func NewClient(baseURL string, creds Credentials, httpClient *http.Client) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = BaseURLEU
	}
	baseURL = strings.TrimSuffix(baseURL, "/v1.0")
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &Client{baseURL: baseURL, creds: creds, http: httpClient}
}

// ─── транспорт ───────────────────────────────────────────────────────────────

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out response, auth string) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("deye: %s %s: ошибка соединения", method, path)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return &APIError{HTTPStatus: resp.StatusCode, Code: "invalid_json"}
	}
	b := out.base()
	if resp.StatusCode >= 400 || !b.Success || (b.Code != "" && string(b.Code) != CodeSuccess) {
		msg := b.Msg
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return &APIError{HTTPStatus: resp.StatusCode, Code: safeIdentifier(string(b.Code)), Msg: msg, RequestID: safeIdentifier(b.RequestID)}
	}
	return nil
}

// call — запрос с токеном; при ошибке авторизации токен обновляется и запрос повторяется один раз.
func (c *Client) call(ctx context.Context, method, path string, body any, out response) error {
	tok, err := c.Token(ctx)
	if err != nil {
		return err
	}
	err = c.do(ctx, method, path, nil, body, out, tok)
	if err != nil && IsAuthError(err) {
		c.InvalidateToken()
		if tok, err = c.Token(ctx); err != nil {
			return err
		}
		*out.base() = BaseResponse{}
		err = c.do(ctx, method, path, nil, body, out, tok)
	}
	return err
}

func (c *Client) post(ctx context.Context, path string, body any, out response) error {
	if body == nil {
		body = map[string]any{}
	}
	return c.call(ctx, http.MethodPost, path, body, out)
}

// ─── Account ─────────────────────────────────────────────────────────────────

type TokenRequest struct {
	AppSecret   string `json:"appSecret"`
	Email       string `json:"email,omitempty"`
	Username    string `json:"username,omitempty"`
	Mobile      string `json:"mobile,omitempty"`
	CountryCode string `json:"countryCode,omitempty"`
	Password    string `json:"password"`
	CompanyID   *int64 `json:"companyId,omitempty"`
}

type TokenResponse struct {
	BaseResponse
	AccessToken  string  `json:"accessToken"`
	RefreshToken string  `json:"refreshToken"`
	TokenType    string  `json:"tokenType"`
	ExpiresIn    FlexInt `json:"expiresIn"`
	Scope        string  `json:"scope"`
	UID          FlexInt `json:"uid"`
}

// ObtainToken — POST /v1.0/account/token?appId=... (без кеша).
func (c *Client) ObtainToken(ctx context.Context) (*TokenResponse, error) {
	if c.creds.AppID == "" || c.creds.AppSecret == "" || c.creds.PasswordSHA256 == "" {
		return nil, errors.New("deye: не заданы appId / appSecret / пароль")
	}
	if c.creds.Email == "" && c.creds.Username == "" && c.creds.Mobile == "" {
		return nil, errors.New("deye: укажите email, username или mobile аккаунта")
	}
	body := TokenRequest{
		AppSecret: c.creds.AppSecret, Email: c.creds.Email, Username: c.creds.Username,
		Mobile: c.creds.Mobile, Password: strings.ToLower(c.creds.PasswordSHA256), CompanyID: c.creds.CompanyID,
	}
	if c.creds.Mobile != "" {
		body.CountryCode = c.creds.CountryCode
	}
	var out TokenResponse
	q := url.Values{"appId": {c.creds.AppID}}
	if err := c.do(ctx, http.MethodPost, "/v1.0/account/token", q, body, &out, ""); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, &APIError{Code: string(out.Code), Msg: "пустой accessToken", RequestID: out.RequestID}
	}
	return &out, nil
}

// Token — заголовок Authorization ("Bearer ..."), с кешем до истечения срока.
func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Now().Before(c.expires) {
		return c.token, nil
	}
	resp, err := c.ObtainToken(ctx)
	if err != nil {
		return "", err
	}
	tok := strings.TrimSpace(resp.AccessToken)
	if !strings.HasPrefix(strings.ToLower(tok), "bearer ") {
		tok = "Bearer " + tok
	}
	ttl := time.Duration(resp.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	// обновляем заранее: за 10% срока, но не больше чем за сутки
	margin := ttl / 10
	if margin > 24*time.Hour {
		margin = 24 * time.Hour
	}
	c.token = tok
	c.expires = time.Now().Add(ttl - margin)
	return c.token, nil
}

// InvalidateToken — сбросить кеш токена.
func (c *Client) InvalidateToken() {
	c.mu.Lock()
	c.token = ""
	c.mu.Unlock()
}

type AccountOrgInfo struct {
	CompanyID   int64  `json:"companyId"`
	CompanyName string `json:"companyName"`
	RoleName    string `json:"roleName"`
}

type AccountInfoResponse struct {
	BaseResponse
	OrgInfoList []AccountOrgInfo `json:"orgInfoList"`
}

// AccountInfo — POST /v1.0/account/info: к какой организации относится аккаунт и его роль.
func (c *Client) AccountInfo(ctx context.Context) (*AccountInfoResponse, error) {
	var out AccountInfoResponse
	return &out, c.post(ctx, "/v1.0/account/info", nil, &out)
}

func safeIdentifier(value string) string {
	if len(value) > 100 || strings.ContainsFunc(value, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r == '-' || r == '_')
	}) {
		return ""
	}
	return value
}
