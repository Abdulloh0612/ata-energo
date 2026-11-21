package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/types"
)

// Логика поиска инверторов: агенты, задачи, найденные устройства, подключение к мониторингу.
//
// Сервер сам ничего не сканирует (он может стоять в другой сети!). Сканирует
// локальный агент (agent/ata_agent.py) и присылает сюда то, что РЕАЛЬНО нашёл.
type DiscoveryService struct {
	agents    repositories.AgentRepository
	tasks     repositories.AgentTaskRepository
	found     repositories.FoundDeviceRepository
	modems    repositories.ModemRepository
	modemSvc  *ModemService
	telemetry *TelemetryService
}

func NewDiscoveryService(
	agents repositories.AgentRepository,
	tasks repositories.AgentTaskRepository,
	found repositories.FoundDeviceRepository,
	modems repositories.ModemRepository,
	modemSvc *ModemService,
	telemetry *TelemetryService,
) *DiscoveryService {
	return &DiscoveryService{agents: agents, tasks: tasks, found: found, modems: modems, modemSvc: modemSvc, telemetry: telemetry}
}

// ─── токены агентов ──────────────────────────────────────────────────────────

// HashAgentToken — SHA-256 от токена (сам токен в базе не храним).
func HashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// NewAgentToken — новый случайный токен (показывается ОДИН раз), его хэш и подсказка (последние 4 символа).
func NewAgentToken() (token, hash, hint string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashAgentToken(token), token[len(token)-4:], nil
}

// AgentCommand — готовая команда запуска агента (для копирования администратором).
func AgentCommand(server, token string) string {
	return fmt.Sprintf("python agent/ata_agent.py --server %s --token %s", strings.TrimRight(server, "/"), token)
}

// ─── агенты (админ) ──────────────────────────────────────────────────────────

func (s *DiscoveryService) ListAgents(ctx context.Context) ([]types.Agent, error) {
	return s.agents.List(ctx)
}

func (s *DiscoveryService) GetAgent(ctx context.Context, id int64) (*types.Agent, error) {
	return s.agents.GetByID(ctx, id)
}

// CreateAgent создаёт агента и возвращает его токен (больше он нигде не показывается).
func (s *DiscoveryService) CreateAgent(ctx context.Context, w types.AgentWrite) (*types.Agent, string, error) {
	name := strings.TrimSpace(str(w.Name))
	if name == "" {
		return nil, "", Field("name", "Обязательное поле.")
	}
	token, hash, hint, err := NewAgentToken()
	if err != nil {
		return nil, "", err
	}
	a := &types.Agent{Name: name, ClientID: w.ClientID, TokenHash: hash, TokenHint: hint, IsActive: true}
	if w.IsActive != nil {
		a.IsActive = *w.IsActive
	}
	if err := s.agents.Create(ctx, a); err != nil {
		return nil, "", err
	}
	saved, err := s.agents.GetByID(ctx, a.ID)
	return saved, token, err
}

func (s *DiscoveryService) UpdateAgent(ctx context.Context, id int64, w types.AgentWrite) (*types.Agent, error) {
	a, err := s.agents.GetByID(ctx, id)
	if err != nil || a == nil {
		return nil, err
	}
	if w.Name != nil {
		if strings.TrimSpace(*w.Name) == "" {
			return nil, Field("name", "Это поле не может быть пустым.")
		}
		a.Name = strings.TrimSpace(*w.Name)
	}
	if w.ClientID != nil {
		a.ClientID = w.ClientID
		if *w.ClientID == 0 {
			a.ClientID = nil
		}
	}
	if w.IsActive != nil {
		a.IsActive = *w.IsActive
	}
	if err := s.agents.Update(ctx, a); err != nil {
		return nil, err
	}
	return s.agents.GetByID(ctx, id)
}

// NewToken выдаёт новый токен (старый перестаёт работать).
func (s *DiscoveryService) NewToken(ctx context.Context, id int64) (string, error) {
	token, hash, hint, err := NewAgentToken()
	if err != nil {
		return "", err
	}
	return token, s.agents.SetToken(ctx, id, hash, hint)
}

func (s *DiscoveryService) DeleteAgent(ctx context.Context, id int64) error {
	return s.agents.Delete(ctx, id)
}

// ─── поиск / проверка / подключение (админ) ──────────────────────────────────

// StartScan создаёт задачу поиска для агента.
func (s *DiscoveryService) StartScan(ctx context.Context, agentID int64, method, subnet string, userID int64) (*types.AgentTask, error) {
	a, err := s.agents.GetByID(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if a == nil || !a.IsActive {
		return nil, Detail(http.StatusNotFound, "Страница не найдена.")
	}
	if method == "" {
		method = types.TaskMethodAll
	}
	if method != types.TaskMethodWiFi && method != types.TaskMethodBluetooth && method != types.TaskMethodAll {
		return nil, Detail(http.StatusBadRequest, "Неизвестный способ поиска.")
	}
	if method == types.TaskMethodBluetooth {
		bt, _ := a.Capabilities["bluetooth"].(map[string]any)
		if avail, _ := bt["available"].(bool); !avail {
			reason, _ := bt["reason"].(string)
			if reason == "" {
				reason = "адаптер или библиотека bleak не найдены"
			}
			return nil, Detail(http.StatusBadRequest, "У этого агента нет Bluetooth: "+reason)
		}
	}
	params := map[string]any{}
	if subnet = strings.TrimSpace(subnet); subnet != "" {
		params["subnet"] = subnet
	}
	t := &types.AgentTask{AgentID: a.ID, Kind: types.TaskKindScan, Method: method, Params: params, CreatedBy: &userID}
	if err := s.tasks.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// TaskDetail — статус задачи и найденные устройства.
func (s *DiscoveryService) TaskDetail(ctx context.Context, id int64) (*types.AgentTask, error) {
	t, err := s.tasks.GetByID(ctx, id)
	if err != nil || t == nil {
		return t, err
	}
	if t.Kind == types.TaskKindScan {
		found, err := s.found.ListByTask(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		t.Found = found
	}
	return t, nil
}

func (s *DiscoveryService) ListDevices(ctx context.Context, agentID *int64) ([]types.FoundDevice, error) {
	return s.found.List(ctx, agentID, 500)
}

func (s *DiscoveryService) GetFound(ctx context.Context, id int64) (*types.FoundDevice, error) {
	return s.found.GetByID(ctx, id)
}

// Probe ставит агенту задачу: проверить, можно ли РЕАЛЬНО прочитать показания.
func (s *DiscoveryService) Probe(ctx context.Context, d *types.FoundDevice, manual map[string]any, userID int64) (*types.AgentTask, error) {
	if d.AgentID == nil {
		return nil, Detail(http.StatusBadRequest, "Устройство найдено браузером. Проверить его может только локальный агент.")
	}
	kind := types.TaskKindProbe
	if d.ConnectionType == "bluetooth" {
		kind = types.TaskKindBLECheck
	}
	ports, _ := d.Details["open_ports"].([]any)
	if ports == nil {
		ports = []any{}
	}
	params := map[string]any{"ip": d.IP, "bt_address": d.BTAddress, "serial": d.Serial, "ports": ports}
	// Ручные настройки администратора (протокол, порт, SN логгера, профиль регистров)
	for _, key := range []string{"protocol", "port", "unit_id", "logger_serial", "profile", "invert_battery", "invert_grid"} {
		if v, ok := manual[key]; ok && v != nil && v != "" {
			params[key] = v
		}
	}
	deviceID := d.ID
	t := &types.AgentTask{AgentID: *d.AgentID, Kind: kind, DeviceID: &deviceID, Params: params, CreatedBy: &userID}
	if err := s.tasks.Create(ctx, t); err != nil {
		return nil, err
	}
	d.Status = types.FoundStatusChecking
	if err := s.found.Save(ctx, d); err != nil {
		return nil, err
	}
	return t, nil
}

// ClearDevices удаляет из списка найденные устройства, которые не подключены.
func (s *DiscoveryService) ClearDevices(ctx context.Context) (int64, error) {
	return s.found.DeleteNotConnected(ctx)
}

// ConnectOptions — параметры подключения найденного устройства к клиенту.
type ConnectOptions struct {
	Site         string
	Name         string
	HasGenerator bool
	PollInterval int
	Options      map[string]any
}

// ModemSerialFor — серийный номер для модема: SN логгера/инвертора, иначе MAC, иначе IP.
func ModemSerialFor(d *types.FoundDevice) string {
	if v, ok := d.CheckResult["serial"]; ok && truthy(v) {
		return strings.TrimSpace(AsString(v))
	}
	if d.Serial != "" {
		return strings.TrimSpace(d.Serial)
	}
	if d.MAC != "" {
		r := strings.NewReplacer(":", "", "-", "")
		return "LAN-" + strings.ToUpper(r.Replace(d.MAC))
	}
	return "LAN-" + strings.ReplaceAll(d.IP, ".", "-")
}

// Connect подключает проверенное устройство к мониторингу.
//
// Создаёт (или обновляет) Modem: агент будет читать показания по найденному
// протоколу и отправлять их на сервер. Дальше работает обычный мониторинг.
func (s *DiscoveryService) Connect(ctx context.Context, d *types.FoundDevice, client *types.Client, opt ConnectOptions) (*types.Modem, error) {
	if ok, _ := d.CheckResult["ok"].(bool); !ok {
		return nil, Detail(http.StatusBadRequest, "Устройство обнаружено, требуется настройка протокола. "+
			"Сначала проверка должна прочитать реальные показания.")
	}
	serial := ModemSerialFor(d)
	if owner, err := s.modems.SerialTaken(ctx, serial, client.ID); err != nil {
		return nil, err
	} else if owner != nil {
		return nil, Detail(http.StatusBadRequest, fmt.Sprintf("Это устройство уже подключено к клиенту «%s».", owner.ClientName))
	}

	connection := map[string]any{}
	if c, ok := d.CheckResult["connection"].(map[string]any); ok {
		for k, v := range c {
			connection[k] = v
		}
	}
	for k, v := range opt.Options {
		connection[k] = v
	}
	poll := opt.PollInterval
	if poll == 0 {
		poll = 10
	}
	connection["poll_interval"] = max(5, min(poll, 300))

	var sections []string
	if raw, ok := d.CheckResult["sections"].([]any); ok {
		for _, x := range raw {
			if sec, ok := x.(string); ok && sec != "generator" {
				sections = append(sections, sec)
			}
		}
	}

	name := opt.Name
	if name == "" {
		var parts []string
		for _, x := range []string{d.Manufacturer, d.Model} {
			if x != "" {
				parts = append(parts, x)
			}
		}
		name = strings.Join(parts, " ")
		if r := []rune(name); len(r) > 100 {
			name = string(r[:100])
		}
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
	m.Site = opt.Site
	m.Name = name
	m.HasGenerator = opt.HasGenerator
	m.AgentID = d.AgentID
	m.Connection = connection
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

	d.ModemID = &m.ID
	d.Status = types.FoundStatusConnected
	if err := s.found.Save(ctx, d); err != nil {
		return nil, err
	}
	return m, nil
}

// BrowserBluetooth сохраняет устройство, найденное в браузере через Web Bluetooth.
func (s *DiscoveryService) BrowserBluetooth(ctx context.Context, data map[string]any) (*types.FoundDevice, error) {
	btID := strings.TrimSpace(AsString(data["id"]))
	if btID == "" {
		return nil, Detail(http.StatusBadRequest, "Нет id устройства")
	}
	var services []string
	if raw, ok := data["services"].([]any); ok {
		for _, x := range raw {
			if len(services) >= 50 {
				break
			}
			services = append(services, AsString(x))
		}
	}
	connected := truthy(data["connected"])
	browserErr, _ := data["error"].(string)
	protocols := []any{"Bluetooth LE"}
	if len(services) > 0 {
		protocols = []any{"Bluetooth LE (GATT)"}
	}
	svcAny := make([]any, len(services))
	for i, x := range services {
		svcAny[i] = x
	}
	d, err := s.UpsertFound(ctx, nil, nil, map[string]any{
		"key":             "bt-browser:" + btID,
		"connection_type": "bluetooth",
		"name":            valueOr(data["name"], ""),
		"bt_address":      btID,
		"device_kind":     "unknown",
		"protocols":       protocols,
		"details": map[string]any{"services": svcAny, "gatt_connected": connected,
			"found_by": "браузер (Web Bluetooth)", "browser_error": browserErr},
	}, "browser")
	if err != nil || d == nil {
		return d, err
	}
	detail := "сервисы не получены"
	if len(services) > 0 {
		detail = fmt.Sprintf("Сервисов: %d", len(services))
	} else if browserErr != "" {
		detail = browserErr
	}
	d.Status = types.FoundStatusNeedsSetup
	d.CheckResult = map[string]any{
		"ok":       false,
		"message":  "Устройство обнаружено, требуется настройка протокола",
		"attempts": []any{map[string]any{"protocol": "Bluetooth GATT (браузер)", "ok": connected, "detail": detail}},
	}
	if err := s.found.Save(ctx, d); err != nil {
		return nil, err
	}
	d.FillDisplay()
	return d, nil
}

// ─── найденные устройства ────────────────────────────────────────────────────

var foundTextFields = []string{"name", "manufacturer", "model", "serial", "ip", "mac", "bt_address", "connection_type"}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// UpsertFound создаёт или обновляет найденное устройство по ключу (ip:... / bt:...).
// Пустые значения не затирают уже известные (например, производителя).
func (s *DiscoveryService) UpsertFound(ctx context.Context, agentID, taskID *int64, item map[string]any, source string) (*types.FoundDevice, error) {
	key := cut(strings.TrimSpace(AsString(item["key"])), 120)
	if key == "" {
		return nil, nil
	}
	connType, _ := item["connection_type"].(string)
	if connType == "" {
		connType = "lan"
	}
	if err := s.found.Create(ctx, &types.FoundDevice{AgentID: agentID, Key: key, ConnectionType: connType, Source: source}); err != nil {
		return nil, err
	}
	d, err := s.found.GetByAgentKey(ctx, agentID, key)
	if err != nil || d == nil {
		return d, err
	}
	for _, f := range foundTextFields {
		v, ok := item[f]
		if !ok || v == nil || v == "" {
			continue
		}
		val := cut(AsString(v), 200)
		switch f {
		case "name":
			d.Name = val
		case "manufacturer":
			d.Manufacturer = val
		case "model":
			d.Model = val
		case "serial":
			d.Serial = cut(val, 100)
		case "ip":
			d.IP = cut(val, 64)
		case "mac":
			d.MAC = cut(val, 32)
		case "bt_address":
			d.BTAddress = cut(val, 120)
		case "connection_type":
			d.ConnectionType = cut(val, 20)
		}
	}
	if v := ToNumber(item["rssi"]); v != nil {
		r := int(*v)
		d.RSSI = &r
	}
	if kind, ok := item["device_kind"].(string); ok {
		if _, valid := types.FoundKindDisplay[kind]; valid && !(kind == types.FoundKindUnknown && d.DeviceKind != types.FoundKindUnknown) {
			d.DeviceKind = kind
		}
	}
	set := map[string]bool{}
	for _, p := range d.Protocols {
		set[p] = true
	}
	if raw, ok := item["protocols"].([]any); ok {
		for _, p := range raw {
			set[AsString(p)] = true
		}
	}
	d.Protocols = sortedKeys(set)
	if det, ok := item["details"].(map[string]any); ok {
		for k, v := range det {
			d.Details[k] = v
		}
	}
	if taskID != nil {
		d.TaskID = taskID
	}
	d.Source = source
	d.LastSeen = time.Now().UTC()
	if err := s.found.Save(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ApplyCheckResult сохраняет результат проверки совместимости.
// result["ok"] = true только если агент РЕАЛЬНО прочитал показания инвертора.
func (s *DiscoveryService) ApplyCheckResult(ctx context.Context, d *types.FoundDevice, result map[string]any) error {
	d.CheckResult = result
	ok, _ := result["ok"].(bool)
	if d.Status != types.FoundStatusConnected {
		if ok {
			d.Status = types.FoundStatusCompatible
		} else {
			d.Status = types.FoundStatusNeedsSetup
		}
	}
	for _, f := range []string{"manufacturer", "model", "serial"} {
		if v, has := result[f]; has && truthy(v) {
			val := cut(AsString(v), 200)
			switch f {
			case "manufacturer":
				d.Manufacturer = val
			case "model":
				d.Model = val
			case "serial":
				d.Serial = cut(val, 100)
			}
		}
	}
	if ok && d.DeviceKind == types.FoundKindUnknown {
		d.DeviceKind = types.FoundKindInverter
	}
	if label, has := result["protocol_label"].(string); has && label != "" {
		set := map[string]bool{label: true}
		for _, p := range d.Protocols {
			set[p] = true
		}
		d.Protocols = sortedKeys(set)
	}
	return s.found.Save(ctx, d)
}

// ─── API агента ──────────────────────────────────────────────────────────────

// AgentByToken находит агента по токену и отмечает, что он на связи. nil — если токен неверный.
func (s *DiscoveryService) AgentByToken(ctx context.Context, token, ip string) (*types.Agent, error) {
	if token == "" {
		return nil, nil
	}
	a, err := s.agents.GetActiveByTokenHash(ctx, HashAgentToken(token))
	if err != nil || a == nil {
		return a, err
	}
	now := time.Now().UTC()
	if err := s.agents.Touch(ctx, a.ID, cut(ip, 64), now); err != nil {
		return nil, err
	}
	a.LastSeen = &now
	a.IP = cut(ip, 64)
	return a, nil
}

// Hello — агент сообщает о себе: имя компьютера, система, есть ли Bluetooth и т.п.
func (s *DiscoveryService) Hello(ctx context.Context, a *types.Agent, data map[string]any) error {
	get := func(k string, n int) string { return cut(AsString(data[k]), n) }
	a.Hostname = get("hostname", 200)
	a.Platform = get("platform", 200)
	a.Version = get("version", 20)
	a.LocalIP = get("local_ip", 64)
	caps, _ := data["capabilities"].(map[string]any)
	a.Capabilities = caps
	return s.agents.SetHello(ctx, a)
}

// Poll отдаёт агенту новые задачи и список устройств, которые надо опрашивать.
func (s *DiscoveryService) Poll(ctx context.Context, a *types.Agent) (map[string]any, error) {
	tasks, err := s.tasks.TakePending(ctx, a.ID, 5)
	if err != nil {
		return nil, err
	}
	modems, err := s.modems.ListByAgent(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	devices := make([]map[string]any, 0)
	for _, m := range modems {
		if m.Protocol() == "" {
			continue
		}
		devices = append(devices, map[string]any{
			"modem_id": m.ID, "serial": m.Serial, "connection": m.Connection,
			"sections": m.Sections, "has_generator": m.HasGenerator,
		})
	}
	out := make([]map[string]any, len(tasks))
	for i, t := range tasks {
		out[i] = map[string]any{"id": t.ID, "kind": t.Kind, "method": t.Method, "params": t.Params}
	}
	return map[string]any{"tasks": out, "devices": devices}, nil
}

// agentTask — задача этого агента или 404.
func (s *DiscoveryService) agentTask(ctx context.Context, a *types.Agent, id int64) (*types.AgentTask, error) {
	t, err := s.tasks.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if t == nil || t.AgentID != a.ID {
		return nil, Detail(http.StatusNotFound, "Страница не найдена.")
	}
	return t, nil
}

func foundItems(data map[string]any) []map[string]any {
	raw, _ := data["found"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// TaskProgress — промежуточный результат: строка в журнал и/или найденные устройства.
func (s *DiscoveryService) TaskProgress(ctx context.Context, a *types.Agent, id int64, data map[string]any) error {
	t, err := s.agentTask(ctx, a, id)
	if err != nil {
		return err
	}
	if line := strings.TrimSpace(AsString(data["log"])); line != "" {
		if err := s.tasks.AppendLog(ctx, t.ID, line); err != nil {
			return err
		}
	}
	for _, item := range foundItems(data) {
		if _, err := s.UpsertFound(ctx, &a.ID, &t.ID, item, "agent"); err != nil {
			return err
		}
	}
	return nil
}

// TaskFinish — итог задачи. Для проверки устройства сохраняет результат чтения показаний.
func (s *DiscoveryService) TaskFinish(ctx context.Context, a *types.Agent, id int64, data map[string]any) error {
	t, err := s.agentTask(ctx, a, id)
	if err != nil {
		return err
	}
	for _, item := range foundItems(data) {
		if _, err := s.UpsertFound(ctx, &a.ID, &t.ID, item, "agent"); err != nil {
			return err
		}
	}
	t.Status = types.TaskStatusDone
	if data["status"] == "error" {
		t.Status = types.TaskStatusError
	}
	t.Error = cut(AsString(data["error"]), 5000)
	t.Result, _ = data["result"].(map[string]any)
	if t.Result == nil {
		t.Result = map[string]any{}
	}
	now := time.Now().UTC()
	t.FinishedAt = &now
	if err := s.tasks.Finish(ctx, t); err != nil {
		return err
	}
	if t.DeviceID != nil && (t.Kind == types.TaskKindProbe || t.Kind == types.TaskKindBLECheck) {
		result := map[string]any{}
		if t.Status == types.TaskStatusError {
			msg := t.Error
			if msg == "" {
				msg = "Ошибка проверки"
			}
			result["ok"] = false
			result["message"] = msg
		}
		for k, v := range t.Result {
			result[k] = v
		}
		d, err := s.found.GetByID(ctx, *t.DeviceID)
		if err != nil {
			return err
		}
		if d != nil {
			return s.ApplyCheckResult(ctx, d, result)
		}
	}
	return nil
}

// AgentTelemetry — показания, прочитанные агентом -> в обычный мониторинг (как сообщение модема).
func (s *DiscoveryService) AgentTelemetry(ctx context.Context, a *types.Agent, data map[string]any) error {
	serial := AsString(data["serial"])
	m, err := s.modems.GetBySerial(ctx, serial)
	if err != nil {
		return err
	}
	if m == nil || m.AgentID == nil || *m.AgentID != a.ID {
		return Detail(http.StatusForbidden, "Этот агент не обслуживает такой модем")
	}
	payload, ok := data["payload"].(map[string]any)
	if !ok {
		return Detail(http.StatusBadRequest, "payload должен быть объектом")
	}
	_, err = s.telemetry.ProcessModemMessage(ctx, serial, payload)
	return err
}
