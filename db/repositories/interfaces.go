package repositories

import (
	"context"
	"time"

	"ata-energo/types"
)

type UserRepository interface {
	GetByID(ctx context.Context, id int64) (*types.User, error)
	GetByUsername(ctx context.Context, username string) (*types.User, error)
	UsernameTaken(ctx context.Context, username string, excludeID int64) (bool, error)
	List(ctx context.Context, f types.UserFilter) ([]types.User, error)
	Create(ctx context.Context, u *types.User) error
	Update(ctx context.Context, u *types.User) error
	Delete(ctx context.Context, id int64) error
	SetLastLogin(ctx context.Context, id int64, t time.Time) error
}

type ClientRepository interface {
	GetByID(ctx context.Context, id int64) (*types.Client, error)
	GetView(ctx context.Context, id int64) (*types.ClientView, error)
	ListViews(ctx context.Context, search string) ([]types.ClientView, error)
	Create(ctx context.Context, c *types.Client) error
	Update(ctx context.Context, c *types.Client) error
	Delete(ctx context.Context, id int64) error
}

type ModemRepository interface {
	GetByID(ctx context.Context, id int64) (*types.Modem, error)
	GetBySerial(ctx context.Context, serial string) (*types.Modem, error)
	GetView(ctx context.Context, id int64, ownerUserID *int64) (*types.ModemView, error)
	ListViews(ctx context.Context, f types.ModemFilter) ([]types.ModemView, error)
	ListByClient(ctx context.Context, clientID int64) ([]types.Modem, error)
	ListByAgent(ctx context.Context, agentID int64) ([]types.Modem, error)
	ListForDemo(ctx context.Context) ([]types.Modem, error)
	ListByProtocol(ctx context.Context, protocol string) ([]types.Modem, error)
	SerialTaken(ctx context.Context, serial string, excludeClientID int64) (*types.Modem, error)
	Create(ctx context.Context, m *types.Modem) error
	Update(ctx context.Context, m *types.Modem) error
	Delete(ctx context.Context, id int64) error
	SetOnline(ctx context.Context, id int64, t time.Time) error
	SetHasGenerator(ctx context.Context, id int64, v bool) error
	ListStaleOnline(ctx context.Context, before, cloudBefore time.Time) ([]types.Modem, error)
	MarkOfflineIfOnline(ctx context.Context, id int64) (bool, error)
}

type DeviceRepository interface {
	GetByID(ctx context.Context, id int64, ownerUserID *int64) (*types.Device, error)
	GetByDeviceID(ctx context.Context, deviceID string) (*types.Device, error)
	List(ctx context.Context, f types.DeviceFilter) ([]types.Device, error)
	ListByModem(ctx context.Context, modemID int64) ([]types.Device, error)
	CountByModem(ctx context.Context, modemID int64) (int, error)
	FirstByModem(ctx context.Context, modemID int64) (*types.Device, error)
	GetOrCreate(ctx context.Context, d *types.Device) (*types.Device, error)
	Create(ctx context.Context, d *types.Device) error
	Update(ctx context.Context, d *types.Device) error
	Delete(ctx context.Context, id int64) error
	DeleteByModemExceptTypes(ctx context.Context, modemID int64, keep []string) error
	DeleteByModemType(ctx context.Context, modemID int64, deviceType string) error
	ReassignModemClient(ctx context.Context, modemID, clientID int64) error
	SaveLast(ctx context.Context, d *types.Device) error
	ListStaleOnline(ctx context.Context, before, cloudBefore time.Time) ([]types.Device, error)
	MarkOfflineIfOnline(ctx context.Context, id int64) (bool, error)
}

type TelemetryRepository interface {
	Insert(ctx context.Context, deviceID int64, ts time.Time, v types.Values, errText *string) error
	History(ctx context.Context, deviceID int64, from, to time.Time, limit int) ([]types.HistoryPoint, int, error)
	ListForOwner(ctx context.Context, ownerUserID int64, deviceID *int64, limit int) ([]types.Telemetry, error)
}

type AlertRepository interface {
	Create(ctx context.Context, deviceID int64, kind, text string) (*types.Alert, error)
	GetByID(ctx context.Context, id int64, ownerUserID *int64) (*types.Alert, error)
	List(ctx context.Context, f types.AlertFilter) ([]types.Alert, error)
	MarkRead(ctx context.Context, id int64) error
	MarkAllRead(ctx context.Context) (int64, error)
}

type AgentRepository interface {
	GetByID(ctx context.Context, id int64) (*types.Agent, error)
	GetActiveByTokenHash(ctx context.Context, hash string) (*types.Agent, error)
	List(ctx context.Context) ([]types.Agent, error)
	Create(ctx context.Context, a *types.Agent) error
	Update(ctx context.Context, a *types.Agent) error
	SetToken(ctx context.Context, id int64, hash, hint string) error
	Touch(ctx context.Context, id int64, ip string, t time.Time) error
	SetHello(ctx context.Context, a *types.Agent) error
	Delete(ctx context.Context, id int64) error
}

type AgentTaskRepository interface {
	GetByID(ctx context.Context, id int64) (*types.AgentTask, error)
	Create(ctx context.Context, t *types.AgentTask) error
	TakePending(ctx context.Context, agentID int64, limit int) ([]types.AgentTask, error)
	AppendLog(ctx context.Context, id int64, line string) error
	Finish(ctx context.Context, t *types.AgentTask) error
}

type FoundDeviceRepository interface {
	GetByID(ctx context.Context, id int64) (*types.FoundDevice, error)
	GetByAgentKey(ctx context.Context, agentID *int64, key string) (*types.FoundDevice, error)
	List(ctx context.Context, agentID *int64, limit int) ([]types.FoundDevice, error)
	ListByTask(ctx context.Context, taskID int64) ([]types.FoundDevice, error)
	Create(ctx context.Context, d *types.FoundDevice) error
	Save(ctx context.Context, d *types.FoundDevice) error
	DeleteNotConnected(ctx context.Context) (int64, error)
}

type VendorAccountRepository interface {
	GetByID(ctx context.Context, id int64) (*types.VendorAccount, error)
	List(ctx context.Context, vendor string) ([]types.VendorAccount, error)
	FindByAppAndLogin(ctx context.Context, vendor, appID, email, username, mobile string) (*types.VendorAccount, error)
	Create(ctx context.Context, a *types.VendorAccount) error
	Update(ctx context.Context, a *types.VendorAccount) error
	SetSyncResult(ctx context.Context, id int64, lastError string, t time.Time) error
	Delete(ctx context.Context, id int64) error
}
