package server

import (
	"ata-energo/db/repositories"
	"ata-energo/services"
)

// Services aggregates all repositories and application services used by the HTTP server.
type Services struct {
	Essentials

	// Repositories accessed directly by handlers
	UserRepo   repositories.UserRepository
	ClientRepo repositories.ClientRepository
	ModemRepo  repositories.ModemRepository
	DeviceRepo repositories.DeviceRepository
	AlertRepo  repositories.AlertRepository
	VendorRepo repositories.VendorAccountRepository

	// Application services
	Auth       *services.AuthService
	Users      *services.UserService
	Clients    *services.ClientService
	Modems     *services.ModemService
	Devices    *services.DeviceService
	Telemetry  *services.TelemetryService
	Discovery  *services.DiscoveryService
	Deye       *services.DeyeService
	Background *services.BackgroundService
	Collector  *services.VendorCollector
	MQTT       *services.MQTTWorker
}

func NewServices(e Essentials) Services {
	userRepo := repositories.NewUserRepo(e.Pool)
	clientRepo := repositories.NewClientRepo(e.Pool)
	modemRepo := repositories.NewModemRepo(e.Pool)
	deviceRepo := repositories.NewDeviceRepo(e.Pool)
	telemetryRepo := repositories.NewTelemetryRepo(e.Pool)
	alertRepo := repositories.NewAlertRepo(e.Pool)
	agentRepo := repositories.NewAgentRepo(e.Pool)
	taskRepo := repositories.NewAgentTaskRepo(e.Pool)
	foundRepo := repositories.NewFoundDeviceRepo(e.Pool)
	vendorRepo := repositories.NewVendorAccountRepo(e.Pool)

	offline := EnvSeconds(e.Vars[DeviceOfflineTimeoutVar], 30)
	cloudOffline := EnvSeconds(e.Vars[DeyeOfflineTimeoutVar], 600)

	modemSvc := services.NewModemService(modemRepo, deviceRepo)
	telemetrySvc := services.NewTelemetryService(modemRepo, deviceRepo, telemetryRepo, alertRepo, modemSvc,
		e.Hub, e.Logger, offline, cloudOffline)
	authSvc := services.NewAuthService(userRepo, clientRepo, e.JWTMgr)
	userSvc := services.NewUserService(userRepo)
	clientSvc := services.NewClientService(clientRepo, userRepo, modemRepo, deviceRepo, modemSvc)
	deviceSvc := services.NewDeviceService(deviceRepo, clientRepo, modemRepo, telemetryRepo)
	discoverySvc := services.NewDiscoveryService(agentRepo, taskRepo, foundRepo, modemRepo, modemSvc, telemetrySvc)
	deyeSvc := services.NewDeyeService(vendorRepo, modemRepo, clientRepo, modemSvc, e.Logger)

	// Драйверы облаков производителей (как в solar-aggregator): сейчас Deye Cloud
	registry := services.NewVendorRegistry()
	if EnvBool(e.Vars[DeyeEnabledVar]) {
		registry.Register(deyeSvc)
	}
	collector := services.NewVendorCollector(registry, modemRepo, telemetrySvc, e.Logger,
		EnvSeconds(e.Vars[DeyePollIntervalVar], 60))

	background := services.NewBackgroundService(telemetrySvc, modemRepo, e.Logger,
		EnvBool(e.Vars[DemoModeVar]), EnvSeconds(e.Vars[DemoIntervalVar], 2))

	var mqttWorker *services.MQTTWorker
	if EnvBool(e.Vars[MQTTEnabledVar]) {
		mqttWorker = services.NewMQTTWorker(services.MQTTConfig{
			Host: e.Vars[MQTTHostVar], Port: e.Vars[MQTTPortVar], Username: e.Vars[MQTTUsernameVar],
			Password: e.Vars[MQTTPasswordVar], Topic: e.Vars[MQTTTopicVar],
		}, telemetrySvc, e.Logger)
	}

	return Services{
		Essentials: e,
		UserRepo:   userRepo,
		ClientRepo: clientRepo,
		ModemRepo:  modemRepo,
		DeviceRepo: deviceRepo,
		AlertRepo:  alertRepo,
		VendorRepo: vendorRepo,
		Auth:       authSvc,
		Users:      userSvc,
		Clients:    clientSvc,
		Modems:     modemSvc,
		Devices:    deviceSvc,
		Telemetry:  telemetrySvc,
		Discovery:  discoverySvc,
		Deye:       deyeSvc,
		Background: background,
		Collector:  collector,
		MQTT:       mqttWorker,
	}
}

// deyeCompanyID — DEYE_COMPANY_ID как число (или nil).
func deyeCompanyID(v string) *int64 {
	var id int64
	for _, r := range v {
		if r < '0' || r > '9' {
			return nil
		}
		id = id*10 + int64(r-'0')
	}
	if id == 0 {
		return nil
	}
	return &id
}
