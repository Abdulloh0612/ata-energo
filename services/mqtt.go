package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"go.uber.org/zap"
)

// MQTTConfig — настройки брокера.
type MQTTConfig struct {
	Host     string
	Port     string
	Username string
	Password string
	Topic    string // по умолчанию inverter/+/data
}

// MQTTWorker принимает данные от модемов по MQTT и сохраняет их:
//  1. подключается к брокеру (с авто-переподключением)
//  2. подписывается на топик inverter/+/data
//  3. каждое сообщение модема передаёт в TelemetryService (база + WebSocket)
type MQTTWorker struct {
	cfg       MQTTConfig
	telemetry *TelemetryService
	log       *zap.Logger
}

func NewMQTTWorker(cfg MQTTConfig, t *TelemetryService, log *zap.Logger) *MQTTWorker {
	if cfg.Topic == "" {
		cfg.Topic = "inverter/+/data"
	}
	if cfg.Port == "" {
		cfg.Port = "1883"
	}
	return &MQTTWorker{cfg: cfg, telemetry: t, log: log}
}

// Run подключается и работает до ctx.Done().
func (w *MQTTWorker) Run(ctx context.Context) {
	opts := mqtt.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%s", w.cfg.Host, w.cfg.Port)).
		SetClientID(fmt.Sprintf("ata-energo-worker-%d", time.Now().Unix())).
		SetKeepAlive(60 * time.Second).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(5 * time.Second).
		SetMaxReconnectInterval(30 * time.Second).
		SetOrderMatters(false)
	if w.cfg.Username != "" {
		opts.SetUsername(w.cfg.Username).SetPassword(w.cfg.Password)
	}
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		// подписываемся заново после каждого (пере)подключения
		tok := c.Subscribe(w.cfg.Topic, 1, w.onMessage)
		tok.Wait()
		if err := tok.Error(); err != nil {
			w.log.Error("MQTT: ошибка подписки", zap.String("topic", w.cfg.Topic), zap.Error(err))
			return
		}
		w.log.Info("MQTT: подключено, подписка", zap.String("topic", w.cfg.Topic))
	})
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		w.log.Warn("MQTT: связь с брокером потеряна, переподключаюсь...", zap.Error(err))
	})

	client := mqtt.NewClient(opts)
	w.log.Info("MQTT: подключаюсь", zap.String("host", w.cfg.Host), zap.String("port", w.cfg.Port))
	client.Connect() // с SetConnectRetry повторяет попытки сам, если брокер ещё не запущен

	<-ctx.Done()
	client.Disconnect(500)
}

func (w *MQTTWorker) onMessage(_ mqtt.Client, msg mqtt.Message) {
	var data any
	if err := json.Unmarshal(msg.Payload(), &data); err != nil {
		p := msg.Payload()
		if len(p) > 200 {
			p = p[:200]
		}
		w.log.Warn("MQTT: не JSON", zap.String("topic", msg.Topic()), zap.ByteString("payload", p))
		return
	}
	// Модем может прислать один объект или список объектов
	items, isList := data.([]any)
	if !isList {
		items = []any{data}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, it := range items {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if err := w.telemetry.ProcessMessage(ctx, item, msg.Topic()); err != nil {
			w.log.Error("MQTT: ошибка обработки сообщения", zap.String("topic", msg.Topic()), zap.Error(err))
		}
	}
}
