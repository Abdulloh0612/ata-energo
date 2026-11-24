package services

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/pkg/valueclouds"
	"ata-energo/types"
)

type ValueCloudsService struct {
	accounts repositories.VendorAccountRepository
	modems   repositories.ModemRepository
	clients  repositories.ClientRepository
	modemSvc *ModemService
	mu       sync.Mutex
	cache    map[int64]cachedValueClouds
}
type cachedValueClouds struct {
	fingerprint string
	client      *valueclouds.Client
}

func NewValueCloudsService(accounts repositories.VendorAccountRepository, modems repositories.ModemRepository, clients repositories.ClientRepository, modemSvc *ModemService) *ValueCloudsService {
	return &ValueCloudsService{accounts: accounts, modems: modems, clients: clients, modemSvc: modemSvc, cache: map[int64]cachedValueClouds{}}
}
func (s *ValueCloudsService) Account(ctx context.Context, id int64) (*types.VendorAccount, *valueclouds.Client, error) {
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if a == nil || a.Vendor != types.VendorValueClouds {
		return nil, nil, Detail(404, "Аккаунт ValueClouds не найден")
	}
	if a.BaseURL != valueclouds.BaseURL {
		return nil, nil, Detail(400, "Неподдерживаемый адрес ValueClouds")
	}
	fp := a.Username + "|" + a.PasswordSHA1
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.cache[id]
	if !ok || cached.fingerprint != fp {
		cached = cachedValueClouds{fp, valueclouds.NewClient(a.BaseURL, valueclouds.Credentials{Account: a.Username, PasswordSHA1: a.PasswordSHA1}, nil)}
		s.cache[id] = cached
	}
	return a, cached.client, nil
}
func (s *ValueCloudsService) SaveAccount(ctx context.Context, id int64, w types.VendorAccountWrite) (*types.VendorAccount, error) {
	a := &types.VendorAccount{Vendor: types.VendorValueClouds, BaseURL: valueclouds.BaseURL, Name: "ValueClouds", IsActive: true}
	if id != 0 {
		var err error
		a, _, err = s.Account(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if w.Name != nil {
		a.Name = strings.TrimSpace(*w.Name)
	}
	if w.Username != nil {
		a.Username = strings.TrimSpace(*w.Username)
	} else if w.Email != nil {
		a.Username = strings.TrimSpace(*w.Email)
	}
	if w.PasswordSHA1 != nil && *w.PasswordSHA1 != "" {
		if w.Password != nil && *w.Password != "" {
			return nil, Field("password_sha1", "Передайте пароль или SHA-1, а не оба значения")
		}
		hashed := strings.ToLower(strings.TrimSpace(*w.PasswordSHA1))
		if data, err := hex.DecodeString(hashed); err != nil || len(data) != 20 {
			return nil, Field("password_sha1", "Укажите SHA-1 из 40 шестнадцатеричных символов")
		}
		a.PasswordSHA1 = hashed
	} else if w.Password != nil && *w.Password != "" {
		a.PasswordSHA1 = valueclouds.HashPassword(*w.Password)
	}
	if w.IsActive != nil {
		a.IsActive = *w.IsActive
	}
	errs := FieldErrors{}
	if a.Name == "" {
		errs["name"] = []string{"Укажите название аккаунта"}
	}
	if a.Username == "" {
		errs["username"] = []string{"Укажите логин ValueClouds"}
	}
	if a.PasswordSHA1 == "" {
		errs["password"] = []string{"Укажите пароль ValueClouds"}
	}
	if w.BaseURL != nil && strings.TrimRight(*w.BaseURL, "/") != valueclouds.BaseURL {
		errs["base_url"] = []string{"Укажите официальный адрес ValueClouds"}
	}
	if len(errs) > 0 {
		return nil, errs
	}
	var err error
	if id == 0 {
		err = s.accounts.Create(ctx, a)
	} else {
		err = s.accounts.Update(ctx, a)
	}
	if err != nil {
		return nil, err
	}
	return s.accounts.GetByID(ctx, a.ID)
}
func (s *ValueCloudsService) ListAccounts(ctx context.Context) ([]types.VendorAccount, error) {
	return s.accounts.List(ctx, types.VendorValueClouds)
}
func (s *ValueCloudsService) DeleteAccount(ctx context.Context, id int64) error {
	if _, _, err := s.Account(ctx, id); err != nil {
		return err
	}
	// Reuse the repository's reference checks; surface no database details.
	if err := s.accounts.Delete(ctx, id); err != nil {
		return Detail(409, "Не удалось удалить аккаунт. Отключите его, чтобы сохранить связанные объекты")
	}
	s.mu.Lock()
	delete(s.cache, id)
	s.mu.Unlock()
	return nil
}
func (s *ValueCloudsService) TestAccount(ctx context.Context, id int64) error {
	_, cl, err := s.Account(ctx, id)
	if err != nil {
		return err
	}
	err = cl.Test(ctx)
	message := ""
	if err != nil {
		message = "Не удалось подключиться к ValueClouds"
	}
	_ = s.accounts.SetSyncResult(ctx, id, message, time.Now().UTC())
	return err
}
func (s *ValueCloudsService) ActiveClient(ctx context.Context, id int64) (*valueclouds.Client, error) {
	a, cl, err := s.Account(ctx, id)
	if err != nil {
		return nil, err
	}
	if !a.IsActive || !a.ProviderEnabled {
		return nil, Detail(409, "Аккаунт или провайдер ValueClouds отключён")
	}
	return cl, nil
}
func valueCloudsPID(d valueclouds.Device) string {
	var text string
	if json.Unmarshal(d.PID, &text) == nil {
		if text == "0" {
			return ""
		}
		return text
	}
	var n json.Number
	if json.Unmarshal(d.PID, &n) == nil && n.String() != "" && n.String() != "0" {
		return n.String()
	}
	return ""
}
func ValueCloudsSiteID(d valueclouds.Device) string {
	if pid := valueCloudsPID(d); pid != "" {
		return "plant:" + pid
	}
	return "device:" + d.Key()
}
func (s *ValueCloudsService) FetchSites(ctx context.Context, id int64) ([]CatalogSite, error) {
	cl, err := s.ActiveClient(ctx, id)
	if err != nil {
		return nil, err
	}
	projects, err := cl.Projects(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := cl.Devices(ctx)
	if err != nil {
		return nil, err
	}
	out := []CatalogSite{}
	seen := map[string]int{}
	hasDevices := map[string]bool{}
	for _, project := range projects {
		key := "plant:" + valueclouds.ProjectID(project)
		name, _ := project["itemName"].(string)
		if name == "" {
			name, _ = project["name"].(string)
		}
		if name == "" {
			name = "Объект " + valueclouds.ProjectID(project)
		}
		address, _ := project["address"].(string)
		seen[key] = len(out)
		out = append(out, CatalogSite{ExternalID: key, Name: name, Address: address, Timezone: "Asia/Bishkek", Status: "empty"})
	}
	for _, d := range devices {
		key := ValueCloudsSiteID(d)
		state := "offline"
		if d.Status == 1 || d.Status == 3 {
			state = "normal"
		} else if d.Status == 2 || d.Status == 4 {
			state = "warning"
		}
		if i, ok := seen[key]; ok {
			if !hasDevices[key] {
				out[i].Status = state
			} else if out[i].Status != state {
				out[i].Status = "warning"
			}
			hasDevices[key] = true
			continue
		}
		name := d.GroupName
		if valueCloudsPID(d) == "" {
			name = d.Name
		}
		if name == "" {
			name = d.SN
		}
		seen[key] = len(out)
		hasDevices[key] = true
		// timezone=0 in device records is not reliable; retain the regional default.
		out = append(out, CatalogSite{ExternalID: key, Name: name, Timezone: "Asia/Bishkek", Status: state})
	}
	return out, nil
}
func (s *ValueCloudsService) SiteDevices(ctx context.Context, accountID int64, externalID string) (*valueclouds.Client, []valueclouds.Device, error) {
	cl, err := s.ActiveClient(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	all, err := cl.Devices(ctx)
	if err != nil {
		return nil, nil, err
	}
	devices := []valueclouds.Device{}
	for _, d := range all {
		if ValueCloudsSiteID(d) == externalID {
			devices = append(devices, d)
		}
	}
	return cl, devices, nil
}
func (s *ValueCloudsService) Device(ctx context.Context, accountID int64, key string) (*valueclouds.Client, *valueclouds.Device, error) {
	cl, err := s.ActiveClient(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	devices, err := cl.Devices(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, d := range devices {
		if d.Key() == key {
			return cl, &d, nil
		}
	}
	return nil, nil, Detail(404, "Устройство ValueClouds не найдено")
}

type ValueCloudsImportRequest struct {
	DeviceKey string `json:"device_key"`
	ClientID  int64  `json:"client"`
	Name      string `json:"name"`
	Site      string `json:"site"`
}

func (s *ValueCloudsService) Import(ctx context.Context, accountID int64, r ValueCloudsImportRequest) (*types.ModemView, error) {
	_, d, err := s.Device(ctx, accountID, r.DeviceKey)
	if err != nil {
		return nil, err
	}
	client, err := s.clients.GetByID(ctx, r.ClientID)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, Field("client", "Клиент не найден")
	}
	// Cloud identity includes collector and address: sn alone need not be globally unique.
	serial := "vc:" + d.Key()
	if len(serial) > 100 {
		return nil, Field("device_key", "Слишком длинный идентификатор устройства")
	}
	if owner, err := s.modems.SerialTaken(ctx, serial, client.ID); err != nil {
		return nil, err
	} else if owner != nil {
		return nil, Detail(409, "Устройство уже подключено к другому клиенту")
	}
	m, err := s.modems.GetBySerial(ctx, serial)
	if err != nil {
		return nil, err
	}
	creating := m == nil
	if creating {
		m = &types.Modem{Serial: serial}
	}
	m.ClientID = client.ID
	m.Name = r.Name
	if m.Name == "" {
		m.Name = d.Name
	}
	if m.Name == "" {
		m.Name = d.SN
	}
	m.Site = r.Site
	m.AgentID = nil
	m.Connection = map[string]any{"protocol": types.ProtocolValueClouds, "account_id": accountID, "device_key": d.Key(), "station_id": ValueCloudsSiteID(*d)}
	m.Sections = []string{"pv", "battery", "grid", "load"}
	if creating {
		err = s.modems.Create(ctx, m)
	} else {
		err = s.modems.Update(ctx, m)
	}
	if err != nil {
		return nil, err
	}
	if err = s.modemSvc.SyncModemDevices(ctx, m); err != nil {
		return nil, err
	}
	return s.modems.GetView(ctx, m.ID, nil)
}
func (s *ValueCloudsService) Protocol() string { return types.ProtocolValueClouds }
func (s *ValueCloudsService) Collect(ctx context.Context, modems []types.Modem) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	var errs []error
	byAccount := map[int64][]types.Modem{}
	for _, m := range modems {
		if id, ok := connInt(m.Connection, "account_id"); ok {
			byAccount[id] = append(byAccount[id], m)
		}
	}
	for id, list := range byAccount {
		cl, err := s.ActiveClient(ctx, id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		devices, err := cl.Devices(ctx)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		index := map[string]valueclouds.Device{}
		for _, d := range devices {
			index[d.Key()] = d
		}
		for _, m := range list {
			key, _ := m.Connection["device_key"].(string)
			d, ok := index[key]
			if !ok || !d.Online() {
				continue
			}
			flow, err := cl.EnergyFlow(ctx, d)
			if err != nil {
				errs = append(errs, fmt.Errorf("аккаунт %d: %w", id, err))
				continue
			}
			payload, err := valueclouds.BuildModemPayload(m.Serial, flow)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			timestamp, _ := payload["timestamp"].(string)
			measured, parseErr := time.Parse(time.RFC3339, timestamp)
			if parseErr != nil || time.Since(measured) > 10*time.Minute || measured.After(time.Now().Add(time.Minute)) {
				continue
			}
			if latest, latestErr := cl.Latest(ctx, d); latestErr == nil {
				valueclouds.EnrichPayload(payload, latest)
			}
			out[m.Serial] = payload
		}
	}
	return out, errors.Join(errs...)
}

// CloudData exposes only read endpoints; callers provide a device selected from this account.
func (s *ValueCloudsService) CloudData(ctx context.Context, cl *valueclouds.Client, d valueclouds.Device, kind, date string, page int) (any, error) {
	q := d.Query()
	path := ""
	switch kind {
	case "latest":
		path = "/ppe/api/auth/web/querySPDeviceLastData"
	case "parameters":
		path = "/ppe/api/auth/web/queryDeviceOneDataxxx"
	case "flow":
		path = "/ppe/api/auth/web/queryDeviceEnergyFlow"
	case "alerts":
		path = "/alm/api/auth/web/device/warnings"
		q.Set("page", strconv.Itoa(page))
		q.Set("pageSize", "100")
	case "D", "M", "Y":
		names := map[string]string{"D": "outputPowerOneDay", "M": "energyMonthPerDay", "Y": "energyYearPerMonth"}
		layouts := map[string]string{"D": "2006-01-02", "M": "2006-01", "Y": "2006"}
		if _, err := time.Parse(layouts[kind], date); err != nil {
			return nil, Field("date", "Укажите корректную дату периода")
		}
		path = "/dcs/api/auth/web/ess/device/" + names[kind]
		q.Set("date", date)
		q.Set("type", "1")
		if kind == "D" {
			q.Set("type", "0")
		}
	default:
		return nil, Detail(400, "Неподдерживаемый тип запроса ValueClouds")
	}
	var result any
	err := cl.Do(ctx, http.MethodGet, path, q, nil, &result)
	return result, err
}

// EnsureEnvAccount bootstraps an encrypted account once, matching the Deye setup.
// Later account edits in CRM are preserved across server restarts.
func (s *ValueCloudsService) EnsureEnvAccount(ctx context.Context, username, passwordSHA1 string) error {
	if username == "" || passwordSHA1 == "" {
		return nil
	}
	existing, err := s.accounts.FindByAppAndLogin(ctx, types.VendorValueClouds, "", "", username, "")
	if err != nil || existing != nil {
		return err
	}
	_, err = s.SaveAccount(ctx, 0, types.VendorAccountWrite{Name: strPtr("ValueClouds (.env)"), Username: &username, PasswordSHA1: &passwordSHA1})
	return err
}
