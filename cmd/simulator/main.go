// Симулятор модема инвертора: шлёт фейковые данные в MQTT, как настоящий модем
// (аналог simulate_modem.py).
//
//	go run ./cmd/simulator/                                   # 2 модема тестовых клиентов (2710000001, 2710000002)
//	go run ./cmd/simulator/ -serials 1234567890               # свой модем (SN под QR-кодом)
//	go run ./cmd/simulator/ -serials 1234567890 -generator    # с генератором
//	go run ./cmd/simulator/ -interval 1 -host 192.168.1.10 -user u -password p
//
// Топик: inverter/<SN модема>/data. Одно сообщение содержит ВСЁ: inverter, pv, battery, grid, generator, load.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ata-energo/pkg/fakestation"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/joho/godotenv"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	_ = godotenv.Load()
	host := flag.String("host", env("MQTT_HOST", "127.0.0.1"), "MQTT host")
	port := flag.String("port", env("MQTT_PORT", "1883"), "MQTT port")
	user := flag.String("user", os.Getenv("MQTT_USERNAME"), "MQTT username")
	password := flag.String("password", os.Getenv("MQTT_PASSWORD"), "MQTT password")
	serials := flag.String("serials", "", "SN модемов через запятую (по умолчанию тестовые)")
	generator := flag.Bool("generator", false, "у модемов из -serials есть генератор")
	stations := flag.Int("stations", 2, "сколько тестовых модемов (если нет -serials)")
	interval := flag.Float64("interval", 2, "пауза между отправками, сек")
	flag.Parse()

	type modem struct {
		serial  string
		station *fakestation.Station
	}
	var modems []modem
	if *serials != "" {
		for _, sn := range strings.Split(*serials, ",") {
			if sn = strings.TrimSpace(sn); sn != "" {
				modems = append(modems, modem{sn, fakestation.New(*generator, 2)})
			}
		}
	} else {
		for i := 1; i <= *stations; i++ {
			modems = append(modems, modem{fakestation.SeedModemSerial(i), fakestation.New(fakestation.SeedHasGenerator(i), 2)})
		}
	}

	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", *host, *port)).
		SetClientID(fmt.Sprintf("fake-modem-%d", time.Now().Unix()))
	if *user != "" {
		opts.SetUsername(*user).SetPassword(*password)
	}
	client := mqtt.NewClient(opts)
	fmt.Printf("Подключаюсь к MQTT %s:%s ...\n", *host, *port)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		log.Fatalf("MQTT: %v", tok.Error())
	}
	defer client.Disconnect(250)

	names := make([]string, len(modems))
	for i, m := range modems {
		names[i] = m.serial
	}
	fmt.Printf("Модемы: %s. Отправка каждые %.1f сек. Остановить: Ctrl+C\n", strings.Join(names, ", "), *interval)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(time.Duration(*interval * float64(time.Second)))
	defer ticker.Stop()
	for {
		for _, m := range modems {
			payload := m.station.ModemPayload(m.serial, *interval)
			data, _ := json.Marshal(payload)
			client.Publish(fmt.Sprintf("inverter/%s/data", m.serial), 1, false, data)
			var sun float64
			for _, p := range payload["pv"].([]any) {
				sun += p.(map[string]any)["power"].(float64)
			}
			grid := "есть"
			if !m.station.GridOn {
				grid = "ОТКЛЮЧЕНА"
			}
			fmt.Printf("%s  модем %s: солнце %.0f Вт, батарея %.0f%%, сеть %s\n",
				time.Now().Format("15:04:05"), m.serial, sun, m.station.Battery, grid)
		}
		select {
		case <-stop:
			fmt.Println("Остановлено.")
			return
		case <-ticker.C:
		}
	}
}
