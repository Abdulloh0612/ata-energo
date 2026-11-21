package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/types"

	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation — нарушение уникальности в PostgreSQL.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// OwnerScope — какие данные видит пользователь:
// admin / it_head / dispatcher -> все (nil), user (клиент) -> только своего клиента (его id).
func OwnerScope(u *types.User) *int64 {
	if u.IsStaffViewer() {
		return nil
	}
	id := u.ID
	return &id
}

// DeviceService — устройства и история для графиков.
type DeviceService struct {
	devices   repositories.DeviceRepository
	clients   repositories.ClientRepository
	modems    repositories.ModemRepository
	telemetry repositories.TelemetryRepository
}

func NewDeviceService(devices repositories.DeviceRepository, clients repositories.ClientRepository,
	modems repositories.ModemRepository, telemetry repositories.TelemetryRepository) *DeviceService {
	return &DeviceService{devices: devices, clients: clients, modems: modems, telemetry: telemetry}
}

func (s *DeviceService) List(ctx context.Context, u *types.User, f types.DeviceFilter) ([]types.DeviceView, error) {
	f.OwnerUserID = OwnerScope(u)
	list, err := s.devices.List(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]types.DeviceView, len(list))
	for i, d := range list {
		out[i] = types.NewDeviceView(d)
	}
	return out, nil
}

// Get — устройство, если оно доступно пользователю (иначе nil -> 404).
func (s *DeviceService) Get(ctx context.Context, u *types.User, id int64) (*types.DeviceView, error) {
	d, err := s.devices.GetByID(ctx, id, OwnerScope(u))
	if err != nil || d == nil {
		return nil, err
	}
	v := types.NewDeviceView(*d)
	return &v, nil
}

func (s *DeviceService) apply(ctx context.Context, d *types.Device, w types.DeviceWrite, creating bool) error {
	errs := FieldErrors{}
	if w.ClientID != nil {
		c, err := s.clients.GetByID(ctx, *w.ClientID)
		if err != nil {
			return err
		}
		if c == nil {
			errs["client"] = []string{"Недопустимый первичный ключ — объект не существует."}
		} else {
			d.ClientID = c.ID
		}
	} else if creating {
		errs["client"] = []string{"Обязательное поле."}
	}
	if w.ModemID != nil {
		m, err := s.modems.GetByID(ctx, *w.ModemID)
		if err != nil {
			return err
		}
		if m == nil {
			errs["modem"] = []string{"Недопустимый первичный ключ — объект не существует."}
		} else {
			d.ModemID = &m.ID
		}
	}
	if w.DeviceType != nil {
		if !types.ValidDeviceType(*w.DeviceType) {
			errs["device_type"] = []string{"Значения «" + *w.DeviceType + "» нет среди допустимых вариантов."}
		}
		d.DeviceType = *w.DeviceType
	} else if creating {
		errs["device_type"] = []string{"Обязательное поле."}
	}
	if w.Name != nil {
		d.Name = *w.Name
	}
	if w.SerialNumber != nil {
		d.SerialNumber = strings.TrimSpace(*w.SerialNumber)
	}
	if w.DeviceID != nil {
		d.DeviceID = strings.TrimSpace(*w.DeviceID)
	}
	if d.SerialNumber == "" {
		errs["serial_number"] = []string{"Обязательное поле."}
	}
	if d.DeviceID == "" {
		errs["device_id"] = []string{"Обязательное поле."}
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

func (s *DeviceService) Create(ctx context.Context, w types.DeviceWrite) (*types.DeviceView, error) {
	d := &types.Device{}
	if err := s.apply(ctx, d, w, true); err != nil {
		return nil, err
	}
	if err := s.devices.Create(ctx, d); err != nil {
		if isUniqueViolation(err) {
			return nil, Field("device_id", "Устройство с таким ID или серийным номером уже существует.")
		}
		return nil, err
	}
	saved, err := s.devices.GetByID(ctx, d.ID, nil)
	if err != nil || saved == nil {
		return nil, err
	}
	v := types.NewDeviceView(*saved)
	return &v, nil
}

func (s *DeviceService) Update(ctx context.Context, id int64, w types.DeviceWrite) (*types.DeviceView, error) {
	d, err := s.devices.GetByID(ctx, id, nil)
	if err != nil || d == nil {
		return nil, err
	}
	if err := s.apply(ctx, d, w, false); err != nil {
		return nil, err
	}
	if err := s.devices.Update(ctx, d); err != nil {
		if isUniqueViolation(err) {
			return nil, Field("device_id", "Устройство с таким ID или серийным номером уже существует.")
		}
		return nil, err
	}
	saved, err := s.devices.GetByID(ctx, id, nil)
	if err != nil || saved == nil {
		return nil, err
	}
	v := types.NewDeviceView(*saved)
	return &v, nil
}

func (s *DeviceService) Delete(ctx context.Context, id int64) error {
	return s.devices.Delete(ctx, id)
}

// History — история показаний для графиков. Если точек слишком много — берём каждую N-ю.
func (s *DeviceService) History(ctx context.Context, d *types.DeviceView, from, to time.Time, limit int) (map[string]any, error) {
	points, total, err := s.telemetry.History(ctx, d.ID, from, to, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"device": d,
		"from":   from,
		"to":     to,
		"total":  total,
		"points": points,
	}, nil
}

// MyTelemetry — последние показания устройств текущего клиента.
func (s *DeviceService) MyTelemetry(ctx context.Context, u *types.User, deviceID *int64, limit int) ([]types.Telemetry, error) {
	return s.telemetry.ListForOwner(ctx, u.ID, deviceID, limit)
}

// ─── Модемы (CRUD) ───────────────────────────────────────────────────────────

func (s *ModemService) List(ctx context.Context, u *types.User, f types.ModemFilter) ([]types.ModemView, error) {
	f.OwnerUserID = OwnerScope(u)
	return s.modems.ListViews(ctx, f)
}

func (s *ModemService) Get(ctx context.Context, u *types.User, id int64) (*types.ModemView, error) {
	var scope *int64
	if u != nil {
		scope = OwnerScope(u)
	}
	return s.modems.GetView(ctx, id, scope)
}

// ModemClients — нужен для проверки клиента при создании модема.
type clientGetter interface {
	GetByID(ctx context.Context, id int64) (*types.Client, error)
}

func (s *ModemService) apply(ctx context.Context, clients clientGetter, m *types.Modem, w types.ModemWrite, creating bool) error {
	errs := FieldErrors{}
	if w.ClientID != nil {
		c, err := clients.GetByID(ctx, *w.ClientID)
		if err != nil {
			return err
		}
		if c == nil {
			errs["client"] = []string{"Недопустимый первичный ключ — объект не существует."}
		} else {
			m.ClientID = c.ID
		}
	} else if creating {
		errs["client"] = []string{"Обязательное поле."}
	}
	if w.Serial != nil {
		m.Serial = strings.TrimSpace(*w.Serial)
		if m.Serial == "" {
			errs["serial"] = []string{"Укажите серийный номер модема."}
		}
	} else if creating {
		errs["serial"] = []string{"Укажите серийный номер модема."}
	}
	if w.Name != nil {
		m.Name = *w.Name
	}
	if w.HasGenerator != nil {
		m.HasGenerator = *w.HasGenerator
	}
	if w.Site != nil {
		m.Site = *w.Site
	}
	if len(errs) > 0 {
		return errs
	}
	return nil
}

// Create создаёт модем и его устройства.
func (s *ModemService) Create(ctx context.Context, clients clientGetter, w types.ModemWrite) (*types.ModemView, error) {
	m := &types.Modem{}
	if err := s.apply(ctx, clients, m, w, true); err != nil {
		return nil, err
	}
	if err := s.modems.Create(ctx, m); err != nil {
		if isUniqueViolation(err) {
			return nil, Field("serial", "Модем с таким Серийный номер модема (под QR-кодом) уже существует.")
		}
		return nil, err
	}
	if err := s.SyncModemDevices(ctx, m); err != nil {
		return nil, err
	}
	return s.modems.GetView(ctx, m.ID, nil)
}

// Update обновляет модем. Если сменили SN — переименовывает устройства.
func (s *ModemService) Update(ctx context.Context, clients clientGetter, id int64, w types.ModemWrite) (*types.ModemView, error) {
	m, err := s.modems.GetByID(ctx, id)
	if err != nil || m == nil {
		return nil, err
	}
	oldSerial := m.Serial
	if err := s.apply(ctx, clients, m, w, false); err != nil {
		return nil, err
	}
	if err := s.modems.Update(ctx, m); err != nil {
		if isUniqueViolation(err) {
			return nil, Field("serial", "Модем с таким Серийный номер модема (под QR-кодом) уже существует.")
		}
		return nil, err
	}
	if m.Serial != oldSerial {
		if err := s.RenameModemDevices(ctx, m, oldSerial); err != nil {
			return nil, err
		}
	}
	if err := s.SyncModemDevices(ctx, m); err != nil {
		return nil, err
	}
	return s.modems.GetView(ctx, m.ID, nil)
}

func (s *ModemService) Delete(ctx context.Context, id int64) error {
	return s.modems.Delete(ctx, id)
}
