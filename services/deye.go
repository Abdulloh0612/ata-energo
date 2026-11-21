package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/pkg/deye"
	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/zap"
)

// DeyeService — интеграция с Deye Cloud OpenAPI.
//
//   - учётные записи Deye (vendor_accounts): CRUD, проверка входа;
//   - просмотр станций и устройств аккаунта, импорт устройства как модема клиента;
//   - драйвер сборщика (VendorAdapter): данные /device/latest + /station/latest
//     превращаются в сообщение модема и идут в обычный мониторинг;
//   - прокси к остальным методам API для конкретного модема (история, аварии, настройки, команды).
//
// Модем Deye хранит в connection:
//
//	{"protocol": "deye_cloud", "account_id": 1, "device_sn": "2401110313", "station_id": 322,
//	 "invert_battery": false, "invert_grid": false, "station_power_scale": 1}
type DeyeService struct {
	accounts repositories.VendorAccountRepository
	modems   repositories.ModemRepository
	clients  repositories.ClientRepository
	modemSvc *ModemService
	log      *zap.Logger

	mu    sync.Mutex
	cache map[int64]*cachedDeye
}

type cachedDeye struct {
	fingerprint string
	client      *deye.Client
}

func NewDeyeService(accounts repositories.VendorAccountRepository, modems repositories.ModemRepository,
	clients repositories.ClientRepository, modemSvc *ModemService, log *zap.Logger) *DeyeService {
	return &DeyeService{accounts: accounts, modems: modems, clients: clients, modemSvc: modemSvc, log: log,
		cache: map[int64]*cachedDeye{}}
}

// ─── учётные записи ──────────────────────────────────────────────────────────

func fingerprint(a *types.VendorAccount) string {
	company := ""
	if a.CompanyID != nil {
		company = strconv.FormatInt(*a.CompanyID, 10)
	}
	return strings.Join([]string{a.BaseURL, a.AppID, a.AppSecret, a.Email, a.Username, a.Mobile, a.CountryCode,
		a.PasswordSHA256, company}, "|")
}

// clientFor — клиент Deye для аккаунта (кешируется, пока данные аккаунта не менялись — токен живёт ~60 дней).
func (s *DeyeService) clientFor(a *types.VendorAccount) *deye.Client {
	fp := fingerprint(a)
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.cache[a.ID]; ok && c.fingerprint == fp {
		return c.client
	}
	cl := deye.NewClient(a.BaseURL, deye.Credentials{
		AppID: a.AppID, AppSecret: a.AppSecret, Email: a.Email, Username: a.Username, Mobile: a.Mobile,
		CountryCode: a.CountryCode, PasswordSHA256: a.PasswordSHA256, CompanyID: a.CompanyID,
	}, nil)
	s.cache[a.ID] = &cachedDeye{fingerprint: fp, client: cl}
	return cl
}

// Account — аккаунт и его клиент.
func (s *DeyeService) Account(ctx context.Context, id int64) (*types.VendorAccount, *deye.Client, error) {
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil || a.Vendor != types.VendorDeye {
		return nil, nil, Detail(http.StatusNotFound, "Аккаунт Deye не найден.")
	}
	if !validDeyeURL(a.BaseURL) {
		return nil, nil, Detail(http.StatusBadRequest, "У аккаунта указан неподдерживаемый адрес Deye Cloud.")
	}
	return a, s.clientFor(a), nil
}

func (s *DeyeService) ListAccounts(ctx context.Context) ([]types.VendorAccount, error) {
	return s.accounts.List(ctx, types.VendorDeye)
}

func validDeyeURL(base string) bool {
	parsed, err := url.Parse(base)
	return err == nil && parsed.Scheme == "https" && parsed.User == nil && parsed.Port() == "" &&
		strings.HasSuffix(strings.ToLower(parsed.Hostname()), "-developer.deyecloud.com") &&
		parsed.RawQuery == "" && parsed.Fragment == "" && (parsed.Path == "" || parsed.Path == "/" || parsed.Path == "/v1.0" || parsed.Path == "/v1.0/")
}

func (s *DeyeService) applyAccount(a *types.VendorAccount, w types.VendorAccountWrite, creating bool) error {
	errs := FieldErrors{}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&a.Name, w.Name)
	set(&a.BaseURL, w.BaseURL)
	set(&a.AppID, w.AppID)
	set(&a.AppSecret, w.AppSecret)
	set(&a.Email, w.Email)
	set(&a.Username, w.Username)
	set(&a.Mobile, w.Mobile)
	set(&a.CountryCode, w.CountryCode)
	if w.CompanyID != nil {
		a.CompanyID = w.CompanyID
		if *w.CompanyID == 0 {
			a.CompanyID = nil
		}
	}
	if w.IsActive != nil {
		a.IsActive = *w.IsActive
	}
	if w.Password != nil && *w.Password != "" {
		a.PasswordSHA256 = deye.HashPassword(*w.Password)
	}
	if a.BaseURL == "" {
		a.BaseURL = deye.BaseURLEU
	}
	if !validDeyeURL(a.BaseURL) {
		errs["base_url"] = []string{"Укажите официальный HTTPS-адрес региона Deye Cloud."}
	}
	if a.Name == "" {
		a.Name = "Deye Cloud"
	}
	if a.AppID == "" {
		errs["app_id"] = []string{"Обязательное поле."}
	}
	if a.AppSecret == "" {
		errs["app_secret"] = []string{"Обязательное поле."}
	}
	if a.PasswordSHA256 == "" {
		errs["password"] = []string{"Обязательное поле."}
	}
	if a.Email == "" && a.Username == "" && a.Mobile == "" {
		errs["email"] = []string{"Укажите email, username или mobile аккаунта Deye."}
	}
	if a.Mobile != "" && a.CountryCode == "" && a.Email == "" && a.Username == "" {
		errs["country_code"] = []string{"Для входа по телефону нужен код страны."}
	}
	_ = creating
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (s *DeyeService) CreateAccount(ctx context.Context, w types.VendorAccountWrite) (*types.VendorAccount, error) {
	a := &types.VendorAccount{Vendor: types.VendorDeye, IsActive: true}
	if err := s.applyAccount(a, w, true); err != nil {
		return nil, err
	}
	if err := s.accounts.Create(ctx, a); err != nil {
		return nil, err
	}
	return s.accounts.GetByID(ctx, a.ID)
}

func (s *DeyeService) UpdateAccount(ctx context.Context, id int64, w types.VendorAccountWrite) (*types.VendorAccount, error) {
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if a == nil || a.Vendor != types.VendorDeye {
		return nil, Detail(http.StatusNotFound, "Аккаунт Deye не найден.")
	}
	if err := s.applyAccount(a, w, false); err != nil {
		return nil, err
	}
	if err := s.accounts.Update(ctx, a); err != nil {
		return nil, err
	}
	return s.accounts.GetByID(ctx, id)
}

func (s *DeyeService) DeleteAccount(ctx context.Context, id int64) error {
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
	err := s.accounts.Delete(ctx, id)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return Detail(http.StatusConflict, "У аккаунта есть объекты в каталоге. Отключите аккаунт, чтобы сохранить историю.")
	}
	return err
}

// EnsureEnvAccount — если в .env заданы DEYE_APP_ID и т.д., создаёт такой аккаунт (один раз).
func (s *DeyeService) EnsureEnvAccount(ctx context.Context, baseURL, appID, appSecret, email, username, mobile, countryCode, password string, companyID *int64) error {
	if appID == "" || appSecret == "" || password == "" || (email == "" && username == "" && mobile == "") {
		return nil
	}
	existing, err := s.accounts.FindByAppAndLogin(ctx, types.VendorDeye, appID, email, username, mobile)
	if err != nil || existing != nil {
		return err
	}
	_, err = s.CreateAccount(ctx, types.VendorAccountWrite{
		Name: strPtr("Deye Cloud (.env)"), BaseURL: &baseURL, AppID: &appID, AppSecret: &appSecret,
		Email: &email, Username: &username, Mobile: &mobile, CountryCode: &countryCode, Password: &password,
		CompanyID: companyID,
	})
	if err == nil {
		s.log.Info("Deye: создан аккаунт из .env", zap.String("source", "environment"))
	}
	return err
}

func strPtr(s string) *string { return &s }

// TestAccount — проверка входа: получает токен и информацию об организации.
func (s *DeyeService) TestAccount(ctx context.Context, id int64) (*deye.AccountInfoResponse, error) {
	a, cl, err := s.Account(ctx, id)
	if err != nil {
		return nil, err
	}
	cl.InvalidateToken()
	info, err := cl.AccountInfo(ctx)
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	_ = s.accounts.SetSyncResult(ctx, a.ID, errText, time.Now().UTC())
	return info, err
}

// ─── импорт устройства Deye как модема клиента ───────────────────────────────

// DeyeImportRequest — тело POST /api/deye/accounts/{id}/import/.
type DeyeImportRequest struct {
	DeviceSn          string   `json:"device_sn"`
	StationID         *int64   `json:"station_id"`
	ClientID          int64    `json:"client"`
	Site              string   `json:"site"`
	Name              string   `json:"name"`
	HasGenerator      bool     `json:"has_generator"`
	Sections          []string `json:"sections"`
	InvertBattery     bool     `json:"invert_battery"`
	InvertGrid        bool     `json:"invert_grid"`
	StationPowerScale *float64 `json:"station_power_scale"`
}

// Import подключает устройство (инвертор) из Deye Cloud к мониторингу: создаёт или обновляет модем
// с connection.protocol = "deye_cloud". SN модема = SN устройства Deye.
func (s *DeyeService) Import(ctx context.Context, accountID int64, r DeyeImportRequest) (*types.ModemView, error) {
	a, _, err := s.Account(ctx, accountID)
	if err != nil {
		return nil, err
	}
	r.DeviceSn = strings.TrimSpace(r.DeviceSn)
	if r.DeviceSn == "" {
		return nil, Field("device_sn", "Укажите SN устройства Deye.")
	}
	client, err := s.clients.GetByID(ctx, r.ClientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, Field("client", "Клиент не найден.")
	}
	if owner, err := s.modems.SerialTaken(ctx, r.DeviceSn, client.ID); err != nil {
		return nil, err
	} else if owner != nil {
		return nil, Detail(http.StatusBadRequest, fmt.Sprintf("Это устройство уже подключено к клиенту «%s».", owner.ClientName))
	}

	conn := map[string]any{
		"protocol":       types.ProtocolDeyeCloud,
		"account_id":     a.ID,
		"device_sn":      r.DeviceSn,
		"invert_battery": r.InvertBattery,
		"invert_grid":    r.InvertGrid,
	}
	if r.StationID != nil {
		conn["station_id"] = *r.StationID
	}
	if r.StationPowerScale != nil {
		conn["station_power_scale"] = *r.StationPowerScale
	}
	var sections []string
	for _, sec := range r.Sections {
		if SectionType(sec) != "" && sec != "generator" {
			sections = append(sections, sec)
		}
	}
	name := r.Name
	if name == "" {
		name = "Deye " + r.DeviceSn
	}

	m, err := s.modems.GetBySerial(ctx, r.DeviceSn)
	if err != nil {
		return nil, err
	}
	creating := m == nil
	if creating {
		m = &types.Modem{Serial: r.DeviceSn}
	}
	m.ClientID = client.ID
	m.Site = r.Site
	m.Name = name
	m.HasGenerator = r.HasGenerator
	m.AgentID = nil
	m.Connection = conn
	m.Sections = sections
	if creating {
		err = s.modems.Create(ctx, m)
	} else {
		err = s.modems.Update(ctx, m)
	}
	if err != nil {
		return nil, err
	}
	if err := s.modemSvc.SyncModemDevices(ctx, m); err != nil {
		return nil, err
	}
	return s.modems.GetView(ctx, m.ID, nil)
}

// ─── доступ к API для конкретного модема ─────────────────────────────────────

// DeyeTarget — модем Deye, его клиент API и параметры.
type DeyeTarget struct {
	Modem     *types.Modem
	Client    *deye.Client
	DeviceSn  string
	StationID *int64
}

func connInt(conn map[string]any, key string) (int64, bool) {
	switch v := conn[key].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	}
	return 0, false
}

func connBool(conn map[string]any, key string) bool {
	v, _ := conn[key].(bool)
	return v
}

func connFloat(conn map[string]any, key string) float64 {
	if p := ToNumber(conn[key]); p != nil {
		return *p
	}
	return 0
}

// Target — модем, подключённый через Deye Cloud (иначе 400).
func (s *DeyeService) Target(ctx context.Context, m *types.Modem) (*DeyeTarget, error) {
	if m.Protocol() != types.ProtocolDeyeCloud {
		return nil, Detail(http.StatusBadRequest, "Модем не подключён через Deye Cloud.")
	}
	accID, ok := connInt(m.Connection, "account_id")
	if !ok {
		return nil, Detail(http.StatusBadRequest, "У модема не указан аккаунт Deye (connection.account_id).")
	}
	_, cl, err := s.Account(ctx, accID)
	if err != nil {
		return nil, err
	}
	t := &DeyeTarget{Modem: m, Client: cl, DeviceSn: m.Serial}
	if sn, _ := m.Connection["device_sn"].(string); sn != "" {
		t.DeviceSn = sn
	}
	if st, ok := connInt(m.Connection, "station_id"); ok {
		t.StationID = &st
	}
	return t, nil
}

// ─── драйвер сборщика (VendorAdapter) ────────────────────────────────────────

func (s *DeyeService) Protocol() string { return types.ProtocolDeyeCloud }

// Collect опрашивает Deye Cloud: /device/latest пачками по 10 + /station/latest по станциям.
func (s *DeyeService) Collect(ctx context.Context, modems []types.Modem) (map[string]map[string]any, error) {
	byAccount := map[int64][]types.Modem{}
	for _, m := range modems {
		if id, ok := connInt(m.Connection, "account_id"); ok {
			byAccount[id] = append(byAccount[id], m)
		}
	}
	out := map[string]map[string]any{}
	var errs []error
	for accID, list := range byAccount {
		a, cl, err := s.Account(ctx, accID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !a.IsActive || !a.ProviderEnabled {
			continue
		}
		res, err := s.collectAccount(ctx, cl, list)
		for k, v := range res {
			out[k] = v
		}
		errText := ""
		if err != nil {
			errText = err.Error()
			errs = append(errs, fmt.Errorf("аккаунт %d: %w", accID, err))
		}
		_ = s.accounts.SetSyncResult(ctx, accID, errText, time.Now().UTC())
	}
	return out, errors.Join(errs...)
}

func (s *DeyeService) collectAccount(ctx context.Context, cl *deye.Client, list []types.Modem) (map[string]map[string]any, error) {
	snToModem := map[string]*types.Modem{}
	var sns []string
	for i := range list {
		m := &list[i]
		sn, _ := m.Connection["device_sn"].(string)
		if sn == "" {
			sn = m.Serial
		}
		snToModem[sn] = m
		sns = append(sns, sn)
	}

	// 1) последние данные устройств, по 10 за запрос
	latest := map[string]*deye.DeviceLatestData{}
	var errs []error
	for i := 0; i < len(sns); i += deye.MaxLatestBatch {
		batch := sns[i:min(i+deye.MaxLatestBatch, len(sns))]
		resp, err := cl.DeviceLatest(ctx, deye.DeviceLatestRequest{DeviceList: batch})
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for j := range resp.DeviceDataList {
			d := &resp.DeviceDataList[j]
			latest[d.DeviceSn] = d
		}
	}

	// 2) данные станций (мощности/заряд — запасной источник)
	stations := map[int64]*deye.StationLatestResponse{}
	for _, m := range snToModem {
		st, ok := connInt(m.Connection, "station_id")
		if !ok {
			continue
		}
		if _, done := stations[st]; done {
			continue
		}
		resp, err := cl.StationLatest(ctx, st)
		if err != nil {
			errs = append(errs, err)
			stations[st] = nil
			continue
		}
		stations[st] = resp
	}

	out := map[string]map[string]any{}
	for sn, m := range snToModem {
		dev := latest[sn]
		var st *deye.StationLatestResponse
		if id, ok := connInt(m.Connection, "station_id"); ok {
			st = stations[id]
		}
		if dev == nil && st == nil {
			continue
		}
		// устройство не на связи с облаком — не шлём данные, модем сам станет offline
		if dev != nil && dev.DeviceState == deye.DeviceStateOffline {
			continue
		}
		if dev == nil && st != nil && st.LastUpdateTime == "" && st.GenerationPower == nil {
			continue
		}
		out[m.Serial] = deye.BuildModemPayload(m.Serial, dev, st, deye.MapOptions{
			InvertBattery:     connBool(m.Connection, "invert_battery"),
			InvertGrid:        connBool(m.Connection, "invert_grid"),
			StationPowerScale: connFloat(m.Connection, "station_power_scale"),
			HasGenerator:      m.HasGenerator,
		})
	}
	return out, errors.Join(errs...)
}
