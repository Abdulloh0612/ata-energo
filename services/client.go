package services

import (
	"context"
	"strings"

	"ata-energo/db/repositories"
	"ata-energo/types"
)

// ClientService — клиенты.
//
// При создании клиента можно сразу:
//   - создать аккаунт для входа (login_username, login_password)
//   - зарегистрировать модем (modem_serial — номер под QR-кодом, modem_name, has_generator).
//     Тогда автоматически создаются инвертор, панели, батарея, сеть, нагрузка
//     и генератор (если has_generator = true).
type ClientService struct {
	clients  repositories.ClientRepository
	users    repositories.UserRepository
	modems   repositories.ModemRepository
	devices  repositories.DeviceRepository
	modemSvc *ModemService
}

func NewClientService(
	clients repositories.ClientRepository,
	users repositories.UserRepository,
	modems repositories.ModemRepository,
	devices repositories.DeviceRepository,
	modemSvc *ModemService,
) *ClientService {
	return &ClientService{clients: clients, users: users, modems: modems, devices: devices, modemSvc: modemSvc}
}

func (s *ClientService) List(ctx context.Context, search string) ([]types.ClientView, error) {
	return s.clients.ListViews(ctx, search)
}

func (s *ClientService) Get(ctx context.Context, id int64) (*types.ClientView, error) {
	return s.clients.GetView(ctx, id)
}

// validate проверяет логин, пароль и серийный номер модема.
func (s *ClientService) validate(ctx context.Context, current *types.Client, w *types.ClientWrite) error {
	username := strings.TrimSpace(str(w.LoginUsername))
	password := str(w.LoginPassword)
	var currentUserID int64
	if current != nil && current.UserID != nil {
		currentUserID = *current.UserID
	}
	if current == nil && strings.TrimSpace(str(w.Name)) == "" {
		return Field("name", "Обязательное поле.")
	}
	if w.Name != nil && strings.TrimSpace(*w.Name) == "" {
		return Field("name", "Это поле не может быть пустым.")
	}
	if username != "" {
		taken, err := s.users.UsernameTaken(ctx, username, currentUserID)
		if err != nil {
			return err
		}
		if taken {
			return Field("login_username", "Такой логин уже занят.")
		}
		if currentUserID == 0 && len([]rune(password)) < 6 {
			return Field("login_password", "Пароль минимум 6 символов.")
		}
	}
	if password != "" && len([]rune(password)) < 6 {
		return Field("login_password", "Пароль минимум 6 символов.")
	}

	serial := strings.TrimSpace(str(w.ModemSerial))
	if serial != "" {
		var exclude int64
		if current != nil {
			exclude = current.ID
		}
		owner, err := s.modems.SerialTaken(ctx, serial, exclude)
		if err != nil {
			return err
		}
		if owner != nil {
			return Field("modem_serial", "Этот модем уже привязан к другому клиенту.")
		}
	}
	w.ModemSerial = &serial
	w.LoginUsername = &username
	return nil
}

// saveLogin создаёт или обновляет аккаунт клиента для входа.
func (s *ClientService) saveLogin(ctx context.Context, c *types.Client, username, password string) error {
	if c.UserID == nil {
		if username == "" {
			return nil
		}
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		first := c.Name
		if r := []rune(first); len(r) > 150 {
			first = string(r[:150])
		}
		u := &types.User{Username: username, PasswordHash: hash, Role: types.RoleUser, FirstName: first,
			Phone: c.Phone, IsActive: true}
		if err := s.users.Create(ctx, u); err != nil {
			return err
		}
		c.UserID = &u.ID
		return s.clients.Update(ctx, c)
	}
	u, err := s.users.GetByID(ctx, *c.UserID)
	if err != nil || u == nil {
		return err
	}
	if username != "" {
		u.Username = username
	}
	if password != "" {
		hash, err := HashPassword(password)
		if err != nil {
			return err
		}
		u.PasswordHash = hash
	}
	return s.users.Update(ctx, u)
}

// saveModem создаёт модем клиента или обновляет первый (основной) модем.
func (s *ClientService) saveModem(ctx context.Context, c *types.Client, serial string, name *string, hasGenerator *bool) error {
	list, err := s.modems.ListByClient(ctx, c.ID)
	if err != nil {
		return err
	}
	var m *types.Modem
	if len(list) == 0 {
		if serial == "" {
			return nil
		}
		m = &types.Modem{ClientID: c.ID, Serial: serial, Name: str(name), HasGenerator: hasGenerator != nil && *hasGenerator}
		if err := s.modems.Create(ctx, m); err != nil {
			return err
		}
	} else {
		m = &list[0]
		oldSerial := m.Serial
		if serial != "" {
			m.Serial = serial
		}
		if name != nil {
			m.Name = *name
		}
		if hasGenerator != nil {
			m.HasGenerator = *hasGenerator
		}
		if err := s.modems.Update(ctx, m); err != nil {
			return err
		}
		if m.Serial != oldSerial {
			if err := s.modemSvc.RenameModemDevices(ctx, m, oldSerial); err != nil {
				return err
			}
		}
	}
	return s.modemSvc.SyncModemDevices(ctx, m)
}

// Create создаёт клиента, его аккаунт и модем с устройствами.
func (s *ClientService) Create(ctx context.Context, w types.ClientWrite) (*types.ClientView, error) {
	if err := s.validate(ctx, nil, &w); err != nil {
		return nil, err
	}
	c := &types.Client{Name: strings.TrimSpace(str(w.Name)), Phone: str(w.Phone), Address: str(w.Address)}
	if err := s.clients.Create(ctx, c); err != nil {
		return nil, err
	}
	if err := s.saveLogin(ctx, c, *w.LoginUsername, str(w.LoginPassword)); err != nil {
		return nil, err
	}
	hasGen := w.HasGenerator
	if hasGen == nil {
		f := false
		hasGen = &f
	}
	if err := s.saveModem(ctx, c, *w.ModemSerial, w.ModemName, hasGen); err != nil {
		return nil, err
	}
	return s.clients.GetView(ctx, c.ID)
}

// Update обновляет клиента, его аккаунт и основной модем.
func (s *ClientService) Update(ctx context.Context, id int64, w types.ClientWrite) (*types.ClientView, error) {
	c, err := s.clients.GetByID(ctx, id)
	if err != nil || c == nil {
		return nil, err
	}
	if err := s.validate(ctx, c, &w); err != nil {
		return nil, err
	}
	if w.Name != nil {
		c.Name = strings.TrimSpace(*w.Name)
	}
	if w.Phone != nil {
		c.Phone = *w.Phone
	}
	if w.Address != nil {
		c.Address = *w.Address
	}
	if err := s.clients.Update(ctx, c); err != nil {
		return nil, err
	}
	if err := s.saveLogin(ctx, c, *w.LoginUsername, str(w.LoginPassword)); err != nil {
		return nil, err
	}
	if err := s.saveModem(ctx, c, *w.ModemSerial, w.ModemName, w.HasGenerator); err != nil {
		return nil, err
	}
	return s.clients.GetView(ctx, c.ID)
}

// Delete удаляет клиента вместе с его аккаунтом для входа.
func (s *ClientService) Delete(ctx context.Context, id int64) error {
	c, err := s.clients.GetByID(ctx, id)
	if err != nil || c == nil {
		return err
	}
	if err := s.clients.Delete(ctx, id); err != nil {
		return err
	}
	if c.UserID != nil {
		u, err := s.users.GetByID(ctx, *c.UserID)
		if err != nil {
			return err
		}
		if u != nil && u.Role == types.RoleUser {
			return s.users.Delete(ctx, u.ID)
		}
	}
	return nil
}

// Devices — всё оборудование клиента.
func (s *ClientService) Devices(ctx context.Context, clientID int64) ([]types.DeviceView, error) {
	list, err := s.devices.List(ctx, types.DeviceFilter{ClientID: &clientID})
	if err != nil {
		return nil, err
	}
	out := make([]types.DeviceView, len(list))
	for i, d := range list {
		out[i] = types.NewDeviceView(d)
	}
	sortDeviceViews(out)
	return out, nil
}

// sortDeviceViews — по типу, затем device_id (как order_by("device_type", "device_id")).
func sortDeviceViews(list []types.DeviceView) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0; j-- {
			a, b := list[j-1], list[j]
			if a.DeviceType < b.DeviceType || (a.DeviceType == b.DeviceType && a.DeviceID <= b.DeviceID) {
				break
			}
			list[j-1], list[j] = b, a
		}
	}
}
