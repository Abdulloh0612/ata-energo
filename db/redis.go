package db

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var redisClient *redis.Client

// GetRedisInstance creates a Redis singleton.
// If an instance was already created, it is returned immediately.
//
// addr может быть "host:port" или "redis://user:pass@host:port/0".
// Пустой addr — Redis не используется (возвращается nil): WebSocket работает
// в памяти одного процесса, как InMemoryChannelLayer в Python-версии.
func GetRedisInstance(addr, password, dbStr string) *redis.Client {
	if redisClient != nil {
		return redisClient
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		log.Println("Redis not configured (REDIS_URL empty) — realtime works inside a single process")
		return nil
	}

	var opts *redis.Options
	if strings.HasPrefix(addr, "redis://") || strings.HasPrefix(addr, "rediss://") {
		parsed, err := redis.ParseURL(addr)
		if err != nil {
			log.Fatalf("parse REDIS_URL: %v", err)
		}
		opts = parsed
		if password != "" {
			opts.Password = password
		}
	} else {
		dbNum, err := strconv.Atoi(dbStr)
		if err != nil || dbNum < 0 {
			dbNum = 0
		}
		opts = &redis.Options{Addr: addr, Password: password, DB: dbNum}
	}

	start := time.Now()
	client := redis.NewClient(opts)

	if err := client.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Could not connect to Redis: %v", err)
	}

	log.Printf("Redis connection established -- %dms", time.Since(start).Milliseconds())
	redisClient = client
	return redisClient
}
