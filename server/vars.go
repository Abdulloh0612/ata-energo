package server

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Env var keys
const (
	AppNameVar    = "APP_NAME"
	AppEnvVar     = "APP_ENV"
	AppBaseURLVar = "APP_BASE_URL"
	TimeZoneVar   = "TIME_ZONE"

	ServerPortVar         = "SERVER_PORT"
	ServerReadTimeoutVar  = "SERVER_READ_TIMEOUT"
	ServerWriteTimeoutVar = "SERVER_WRITE_TIMEOUT"

	DatabaseURLVar       = "DATABASE_URL"
	DBMaxConnsVar        = "DB_MAX_CONNS"
	DBMinConnsVar        = "DB_MIN_CONNS"
	DBMaxConnLifetimeVar = "DB_MAX_CONN_LIFETIME"

	RedisURLVar      = "REDIS_URL"
	RedisPasswordVar = "REDIS_PASSWORD"
	RedisDBVar       = "REDIS_DB"

	JWTSecretVar     = "JWT_SECRET"
	JWTAccessTTLVar  = "JWT_ACCESS_TTL"
	JWTRefreshTTLVar = "JWT_REFRESH_TTL"

	// MQTT (модемы)
	MQTTEnabledVar  = "MQTT_ENABLED"
	MQTTHostVar     = "MQTT_HOST"
	MQTTPortVar     = "MQTT_PORT"
	MQTTUsernameVar = "MQTT_USERNAME"
	MQTTPasswordVar = "MQTT_PASSWORD"
	MQTTTopicVar    = "MQTT_TOPIC"

	// Мониторинг
	DeviceOfflineTimeoutVar = "DEVICE_OFFLINE_TIMEOUT" // секунд без данных -> offline
	DemoModeVar             = "DEMO_MODE"
	DemoIntervalVar         = "DEMO_INTERVAL" // секунд

	// Deye Cloud OpenAPI
	DeyeEnabledVar        = "DEYE_ENABLED"
	DeyeBaseURLVar        = "DEYE_BASE_URL"
	DeyeAppIDVar          = "DEYE_APP_ID"
	DeyeAppSecretVar      = "DEYE_APP_SECRET"
	DeyeEmailVar          = "DEYE_EMAIL"
	DeyeUsernameVar       = "DEYE_USERNAME"
	DeyeMobileVar         = "DEYE_MOBILE"
	DeyeCountryCodeVar    = "DEYE_COUNTRY_CODE"
	DeyePasswordVar       = "DEYE_PASSWORD"
	DeyeCompanyIDVar      = "DEYE_COMPANY_ID"
	DeyePollIntervalVar   = "DEYE_POLL_INTERVAL"   // секунд между опросами облака
	DeyeOfflineTimeoutVar = "DEYE_OFFLINE_TIMEOUT" // секунд без данных из облака -> offline
)

// LoadEnvVars loads environment variables into memory to avoid repeated syscalls.
// Values are initialized once and only read afterwards.
func LoadEnvVars() map[string]string {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: could not load .env file, using environment variables")
	}

	keys := []string{
		AppNameVar, AppEnvVar, AppBaseURLVar, TimeZoneVar,
		ServerPortVar, ServerReadTimeoutVar, ServerWriteTimeoutVar,
		DatabaseURLVar, DBMaxConnsVar, DBMinConnsVar, DBMaxConnLifetimeVar,
		RedisURLVar, RedisPasswordVar, RedisDBVar,
		JWTSecretVar, JWTAccessTTLVar, JWTRefreshTTLVar,
		MQTTEnabledVar, MQTTHostVar, MQTTPortVar, MQTTUsernameVar, MQTTPasswordVar, MQTTTopicVar,
		DeviceOfflineTimeoutVar, DemoModeVar, DemoIntervalVar,
		DeyeEnabledVar, DeyeBaseURLVar, DeyeAppIDVar, DeyeAppSecretVar, DeyeEmailVar, DeyeUsernameVar,
		DeyeMobileVar, DeyeCountryCodeVar, DeyePasswordVar, DeyeCompanyIDVar, DeyePollIntervalVar,
		DeyeOfflineTimeoutVar,
	}

	vars := make(map[string]string, len(keys))
	for _, k := range keys {
		vars[k] = os.Getenv(k)
	}

	// Apply defaults for missing values
	defaults := map[string]string{
		AppNameVar:              "ata-energo",
		AppEnvVar:               "development",
		AppBaseURLVar:           "http://localhost:8000",
		TimeZoneVar:             "Asia/Bishkek",
		ServerPortVar:           "8000",
		ServerReadTimeoutVar:    "15s",
		ServerWriteTimeoutVar:   "30s",
		DBMaxConnsVar:           "20",
		DBMinConnsVar:           "2",
		DBMaxConnLifetimeVar:    "1h",
		RedisDBVar:              "0",
		JWTAccessTTLVar:         "60m",
		JWTRefreshTTLVar:        "168h",
		MQTTEnabledVar:          "false",
		MQTTHostVar:             "127.0.0.1",
		MQTTPortVar:             "1883",
		MQTTTopicVar:            "inverter/+/data",
		DeviceOfflineTimeoutVar: "30",
		DemoModeVar:             "false",
		DemoIntervalVar:         "2",
		DeyeEnabledVar:          "true",
		DeyeBaseURLVar:          "https://eu1-developer.deyecloud.com",
		DeyePollIntervalVar:     "180",
		DeyeOfflineTimeoutVar:   "600",
	}
	for k, def := range defaults {
		if vars[k] == "" {
			vars[k] = def
		}
	}

	return vars
}

// EnvBool — да/нет из .env (true/false, 1/0, yes/no, on/off, да).
func EnvBool(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on", "да":
		return true
	}
	return false
}

// EnvSeconds — число секунд из .env (или def).
func EnvSeconds(v string, def int) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		n = def
	}
	return time.Duration(n) * time.Second
}
