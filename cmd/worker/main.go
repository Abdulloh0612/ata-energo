// Отдельный процесс-воркер (аналог python manage.py run_mqtt и check_offline).
//
// Обычно не нужен: веб-сервер сам запускает MQTT (MQTT_ENABLED=true), проверку offline
// и опрос Deye Cloud. Отдельный воркер удобен, если сайт запущен в нескольких экземплярах —
// тогда фоновые задачи держит один воркер, а данные до сайтов доходят через Redis (REDIS_URL).
//
//	go run ./cmd/worker/              # MQTT + проверка offline + Deye Cloud
//	go run ./cmd/worker/ -once        # один раз проверить offline (удобно для cron)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"ata-energo/server"
)

func main() {
	once := flag.Bool("once", false, "один раз проверить устройства без связи и выйти")
	flag.Parse()

	e := server.NewEssentials()
	defer e.Pool.Close()
	s, err := server.NewServices(e)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *once {
		n, err := s.Telemetry.CheckOfflineDevices(ctx)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Стали offline: %d\n", n)
		return
	}

	if e.Redis == nil {
		e.Logger.Warn("REDIS_URL не задан: данные будут сохраняться в базу, но НЕ попадут в WebSocket сайта в реальном времени.")
	}
	if s.MQTT != nil {
		go s.MQTT.Run(ctx)
	} else {
		e.Logger.Warn("MQTT_ENABLED=false — MQTT не запущен")
	}
	if server.EnvBool(e.Vars[server.DeyeEnabledVar]) {
		go s.Collector.Run(ctx)
	}
	s.Background.Run(ctx) // проверка offline (+ демо, если DEMO_MODE=true)
}
