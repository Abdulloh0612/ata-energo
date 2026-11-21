package server

import (
	"context"
	"log"
	"time"

	"ata-energo/db"
	"ata-energo/pkg/hub"
	jwtpkg "ata-energo/pkg/jwt"
	"ata-energo/pkg/logger"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Essentials holds the core initialized dependencies shared across the app.
type Essentials struct {
	Pool     *pgxpool.Pool
	Redis    *redis.Client // nil, если REDIS_URL не задан
	Logger   *zap.Logger
	JWTMgr   *jwtpkg.Manager
	Hub      *hub.Hub
	Location *time.Location
	Vars     map[string]string
}

// NewEssentials initializes all core dependencies.
func NewEssentials() Essentials {
	vars := LoadEnvVars()

	zapLogger, err := logger.New(vars[AppEnvVar])
	if err != nil {
		panic("failed to create logger: " + err.Error())
	}

	if vars[JWTSecretVar] == "" {
		if vars[AppEnvVar] == "production" {
			log.Fatal("JWT_SECRET is required in production")
		}
		vars[JWTSecretVar] = "dev-only-secret-change-me"
		zapLogger.Warn("JWT_SECRET не задан — используется ключ для разработки")
	}

	loc, err := time.LoadLocation(vars[TimeZoneVar])
	if err != nil {
		zapLogger.Warn("неизвестный TIME_ZONE, используется UTC", zap.String("tz", vars[TimeZoneVar]))
		loc = time.UTC
	}

	pool := db.GetPoolInstance(
		context.Background(),
		vars[DatabaseURLVar],
		vars[DBMaxConnsVar],
		vars[DBMinConnsVar],
		1,
	)

	rdb := db.GetRedisInstance(vars[RedisURLVar], vars[RedisPasswordVar], vars[RedisDBVar])

	jwtMgr := jwtpkg.NewManagerFromVars(
		vars[JWTSecretVar],
		vars[JWTAccessTTLVar],
		vars[JWTRefreshTTLVar],
	)

	return Essentials{
		Pool:     pool,
		Redis:    rdb,
		Logger:   zapLogger,
		JWTMgr:   jwtMgr,
		Hub:      hub.New(rdb, zapLogger),
		Location: loc,
		Vars:     vars,
	}
}
