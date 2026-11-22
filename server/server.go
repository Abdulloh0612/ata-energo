package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	agenthandler "ata-energo/handlers/agent"
	apihandler "ata-energo/handlers/api"
	wshandler "ata-energo/handlers/ws"
	"ata-energo/middleware"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/swagger"
	"go.uber.org/zap"
)

// @title Ата Энерго API
// @version 1.0
// @description Мониторинг солнечных станций: панели → инвертор → батарея → генератор → сеть → нагрузка. Данные модемов (MQTT), локальных агентов и Deye Cloud.
// @description Для проверки запросов выполните POST /auth/login и вставьте `Bearer <access>` в Authorize. Для локальных агентов используется X-Agent-Token.
// @description Deye Cloud: получите локальный ID через GET /deye/accounts, проверьте подключение через POST /deye/accounts/{id}/test и откройте GET /deye/accounts/{id}/stations. Для телеметрии нужен ID подключённого модема из GET /modems.
// @description Поток телеметрии доступен через WebSocket `/ws/telemetry?token=<access>`.
// @accept json
// @produce json
// @host localhost:8000
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description `Bearer <access>` из ответа /api/auth/login
// @securityDefinitions.apikey AgentToken
// @in header
// @name X-Agent-Token

type Server struct {
	app      *fiber.App
	services Services
	cancel   context.CancelFunc
}

func New() (*Server, error) {
	e := NewEssentials()
	applySwaggerFromBaseURL(e.Vars[AppBaseURLVar])
	s, err := NewServices(e)
	if err != nil {
		e.Pool.Close()
		return nil, err
	}

	app := fiber.New(fiber.Config{
		ReadTimeout:  parseDurationOrDefault(e.Vars[ServerReadTimeoutVar], 15*time.Second),
		WriteTimeout: parseDurationOrDefault(e.Vars[ServerWriteTimeoutVar], 30*time.Second),
		AppName:      "Ata Energo API",
		BodyLimit:    8 * 1024 * 1024,
		ErrorHandler: errorHandler(e.Logger),
	})

	app.Use(recover.New())
	app.Use(fiberlogger.New(fiberlogger.Config{
		Next: func(c *fiber.Ctx) bool {
			return strings.HasPrefix(c.Path(), "/ws/")
		},
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Agent-Token",
		AllowMethods: "GET, POST, PUT, PATCH, DELETE, OPTIONS",
	}))

	app.Get("/swagger/*", swagger.New(swagger.Config{InstanceName: "swagger"}))

	registerRoutes(app, s)

	// Vite emits content-hashed assets. Only browser navigation receives the SPA shell.
	app.Get("/station-dashboard.html", func(c *fiber.Ctx) error {
		return c.SendFile("web/dist/station-dashboard.html")
	})
	app.Static("/assets", "web/dist/assets", fiber.Static{MaxAge: 31536000})
	app.Get("*", func(c *fiber.Ctx) error {
		if strings.Contains(c.Path(), ".") || strings.HasPrefix(c.Path(), "/assets/") {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderCacheControl, "no-cache")
		return c.SendFile("web/dist/index.html")
	})

	return &Server{app: app, services: s}, nil
}

func registerRoutes(app *fiber.App, s Services) {
	authH := apihandler.NewAuthHandler(s.Auth, s.Users)
	clientH := apihandler.NewClientHandler(s.Clients)
	deviceH := apihandler.NewDeviceHandler(s.Devices, s.Modems, s.ClientRepo, s.Location)
	alertH := apihandler.NewAlertHandler(s.AlertRepo)
	discoveryH := apihandler.NewDiscoveryHandler(s.Discovery, s.ClientRepo)
	deyeH := apihandler.NewDeyeHandler(s.Deye, s.Modems, s.Collector, s.Location)
	agentH := agenthandler.NewHandler(s.Discovery)
	wsH := wshandler.NewHandler(s.Auth, s.Hub, s.Logger)

	// ── WebSocket ─────────────────────────────────────────────────────────────
	app.Use("/ws", wsH.Upgrade)
	app.Get("/ws/telemetry", wsH.Telemetry())

	api := app.Group("/api")
	// @Summary Проверить доступность API
	// @Tags Health
	// @Success 200 {object} map[string]string
	// @Router /health [get]
	api.Get("/health", func(c *fiber.Ctx) error { return c.JSON(fiber.Map{"status": "ok"}) })

	// ── Вход ──────────────────────────────────────────────────────────────────
	api.Post("/auth/login", authH.Login)
	api.Post("/auth/refresh", authH.Refresh)

	// ── API локального агента (X-Agent-Token, без JWT) ────────────────────────
	// middleware на каждом маршруте: групповой Use("/api/agent") в Fiber совпал бы и с /api/agents
	agentAuth := middleware.AgentAuth(s.Discovery)
	agent := api.Group("/agent")
	agent.Post("/hello", agentAuth, agentH.Hello)
	agent.Get("/poll", agentAuth, agentH.Poll)
	agent.Post("/tasks/:id/progress", agentAuth, agentH.Progress)
	agent.Post("/tasks/:id/finish", agentAuth, agentH.Finish)
	agent.Post("/telemetry", agentAuth, agentH.Telemetry)

	// ── Всё остальное — с JWT ─────────────────────────────────────────────────
	jwt := middleware.JWTAuth(s.Auth)
	manager := middleware.ManagerOnly()

	api.Get("/me", jwt, authH.Me)
	apihandler.RegisterCRM(api.Group("/crm", jwt), s.Pool)
	apihandler.RegisterProviders(api.Group("/crm", jwt), s.Pool, s.Catalog, s.Deye)

	users := api.Group("/users", jwt, manager)
	users.Get("", authH.ListUsers)
	users.Post("", middleware.AdminOnly(), authH.CreateUser)
	users.Get("/:id", authH.GetUser)
	users.Patch("/:id", middleware.AdminOnly(), authH.UpdateUser)
	users.Put("/:id", middleware.AdminOnly(), authH.UpdateUser)
	users.Delete("/:id", middleware.AdminOnly(), authH.DeleteUser)

	clients := api.Group("/clients", jwt, middleware.CRMInventoryAccess(true))
	clients.Get("", clientH.List)
	clients.Post("", clientH.Create)
	clients.Get("/:id", clientH.Get)
	clients.Patch("/:id", clientH.Update)
	clients.Put("/:id", clientH.Update)
	clients.Delete("/:id", clientH.Delete)
	clients.Get("/:id/devices", clientH.Devices)

	devices := api.Group("/devices", jwt, middleware.CRMInventoryAccess(false))
	devices.Get("", deviceH.ListDevices)
	devices.Post("", deviceH.CreateDevice)
	devices.Get("/:id", deviceH.GetDevice)
	devices.Patch("/:id", deviceH.UpdateDevice)
	devices.Put("/:id", deviceH.UpdateDevice)
	devices.Delete("/:id", deviceH.DeleteDevice)
	devices.Get("/:id/history", deviceH.History)

	my := api.Group("/my", jwt, middleware.ClientOnly())
	apihandler.RegisterCustomerPortal(my, s.Pool)
	my.Get("/devices", deviceH.MyDevices)
	my.Get("/telemetry", deviceH.MyTelemetry)

	// Модемы: чтение — всем (каждый видит своё), изменение — admin/it_head.
	// Права заданы на каждом маршруте: POST-запросы истории Deye — это чтение.
	modems := api.Group("/modems", jwt)
	modems.Get("", deviceH.ListModems)
	modems.Post("", manager, deviceH.CreateModem)
	modems.Get("/:id", deviceH.GetModem)
	modems.Patch("/:id", manager, deviceH.UpdateModem)
	modems.Put("/:id", manager, deviceH.UpdateModem)
	modems.Delete("/:id", manager, deviceH.DeleteModem)

	// Deye Cloud по модему
	staff := middleware.StaffOnly()
	modems.Get("/:id/deye/latest", deyeH.Latest)
	modems.Get("/:id/deye/station", deyeH.StationLatest)
	modems.Get("/:id/deye/measure-points", deyeH.MeasurePoints)
	modems.Get("/:id/deye/alerts", deyeH.Alerts)
	modems.Get("/:id/deye/station-alerts", deyeH.StationAlerts)
	modems.Post("/:id/deye/history", deyeH.History)
	modems.Post("/:id/deye/history-raw", deyeH.HistoryRaw)
	modems.Post("/:id/deye/station-history", deyeH.StationHistory)
	modems.Post("/:id/deye/station-history-power", deyeH.StationHistoryPower)
	modems.Get("/:id/deye/config", staff, deyeH.Config)
	modems.Get("/:id/deye/orders/:orderId", staff, deyeH.Order)
	modems.Get("/:id/deye/dynamic/result/:orderId", staff, deyeH.DynamicResult)
	modems.Post("/:id/deye/control/:command", middleware.AdminOnly(), deyeH.Control)
	modems.Post("/:id/deye/dynamic/read", middleware.AdminOnly(), deyeH.DynamicRead)

	alerts := api.Group("/alerts", jwt)
	alerts.Get("", alertH.List)
	alerts.Post("/read_all", alertH.ReadAll)
	alerts.Get("/:id", alertH.Get)
	alerts.Post("/:id/read", alertH.Read)

	// ── Поиск инверторов (admin, it_head) ─────────────────────────────────────
	agents := api.Group("/agents", jwt, manager)
	agents.Get("", discoveryH.ListAgents)
	agents.Post("", discoveryH.CreateAgent)
	agents.Get("/:id", discoveryH.GetAgent)
	agents.Patch("/:id", discoveryH.UpdateAgent)
	agents.Put("/:id", discoveryH.UpdateAgent)
	agents.Delete("/:id", discoveryH.DeleteAgent)
	agents.Post("/:id/new_token", discoveryH.NewToken)

	disc := api.Group("/discovery", jwt, manager)
	disc.Post("/scan", discoveryH.StartScan)
	disc.Get("/tasks/:id", discoveryH.TaskDetail)
	disc.Get("/devices", discoveryH.DeviceList)
	disc.Post("/devices/clear", discoveryH.DevicesClear)
	disc.Post("/devices/:id/probe", discoveryH.DeviceProbe)
	disc.Post("/devices/:id/connect", discoveryH.DeviceConnect)
	disc.Post("/browser-bluetooth", discoveryH.BrowserBluetooth)

	// ── Deye Cloud: аккаунты (admin) ─────────────────────────────────
	dy := api.Group("/deye", jwt, middleware.AdminOnly())
	dy.Post("/sync", deyeH.Sync)
	dy.Get("/accounts", deyeH.ListAccounts)
	dy.Post("/accounts", deyeH.CreateAccount)
	dy.Get("/accounts/:id", deyeH.GetAccount)
	dy.Patch("/accounts/:id", deyeH.UpdateAccount)
	dy.Put("/accounts/:id", deyeH.UpdateAccount)
	dy.Delete("/accounts/:id", deyeH.DeleteAccount)
	dy.Post("/accounts/:id/test", deyeH.TestAccount)
	dy.Get("/accounts/:id/stations", deyeH.Stations)
	dy.Post("/accounts/:id/stations/create", deyeH.StationCreate)
	dy.Get("/accounts/:id/stations/:sid", deyeH.Station)
	dy.Get("/accounts/:id/stations/:sid/devices", deyeH.StationDevices)
	dy.Get("/accounts/:id/devices", deyeH.Devices)
	dy.Post("/accounts/:id/devices/register", deyeH.DeviceRegister)
	dy.Post("/accounts/:id/loggers/:action", deyeH.Loggers)
	dy.Post("/accounts/:id/import", deyeH.Import)

	api.Use(func(c *fiber.Ctx) error { // неизвестный /api/... — JSON 404
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"detail": "Страница не найдена."})
	})
}

// errorHandler — непредвиденные ошибки: в лог, клиенту — {"detail": ...}.
func errorHandler(log *zap.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		msg := "Внутренняя ошибка сервера"
		var fe *fiber.Error
		if errors.As(err, &fe) {
			code = fe.Code
			msg = fe.Message
		} else {
			log.Error("request failed", zap.String("method", c.Method()), zap.String("path", c.Path()), zap.Error(err))
		}
		return c.Status(code).JSON(fiber.Map{"detail": msg})
	}
}

// startBackground — фоновые задачи: проверка offline (+ демо), MQTT, опрос облаков, Redis-подписка.
func (s *Server) startBackground(ctx context.Context) {
	e := s.services
	go e.Hub.Run(ctx)
	go e.Background.Run(ctx)
	if e.MQTT != nil {
		go e.MQTT.Run(ctx)
	} else {
		e.Logger.Info("MQTT выключен (MQTT_ENABLED=false)")
	}
	if EnvBool(e.Vars[DeyeEnabledVar]) {
		if err := e.Deye.EnsureEnvAccount(ctx, e.Vars[DeyeBaseURLVar], e.Vars[DeyeAppIDVar], e.Vars[DeyeAppSecretVar],
			e.Vars[DeyeEmailVar], e.Vars[DeyeUsernameVar], e.Vars[DeyeMobileVar], e.Vars[DeyeCountryCodeVar],
			e.Vars[DeyePasswordVar], deyeCompanyID(e.Vars[DeyeCompanyIDVar])); err != nil {
			e.Logger.Warn("Deye: не удалось создать аккаунт из .env", zap.Error(err))
		}
		go e.Collector.Run(ctx)
		go e.Catalog.Run(ctx)
	}
}

func (s *Server) Run() error {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.startBackground(ctx)

	port := s.services.Vars[ServerPortVar]
	go func() {
		if err := s.app.Listen(fmt.Sprintf(":%s", port)); err != nil {
			log.Fatalf("server listen: %v", err)
		}
	}()

	s.services.Logger.Sugar().Infof("Server started on :%s", port)
	<-quit
	s.services.Logger.Info("Shutting down...")
	cancel()
	return s.app.ShutdownWithTimeout(10 * time.Second)
}

func (s *Server) Close() {
	if s.cancel != nil {
		s.cancel()
	}
	if s.services.Pool != nil {
		s.services.Pool.Close()
	}
	if s.services.Redis != nil {
		_ = s.services.Redis.Close()
	}
	if s.services.Logger != nil {
		_ = s.services.Logger.Sync()
	}
}

func parseDurationOrDefault(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
