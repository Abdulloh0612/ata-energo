package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/types"

	"go.uber.org/zap"
)

// VendorAdapter — контракт драйвера облака производителя (как в solar-aggregator).
// Драйвер получает модемы своего протокола и возвращает сообщения в формате модема,
// дальше они идут в обычный мониторинг (ProcessModemMessage).
type VendorAdapter interface {
	// Protocol — значение connection.protocol модемов этого драйвера (например, "deye_cloud").
	Protocol() string
	// Collect опрашивает облако. Результат: SN модема -> сообщение модема.
	// Модемы, по которым облако не дало данных (offline), в результат не попадают.
	Collect(ctx context.Context, modems []types.Modem) (map[string]map[string]any, error)
}

// VendorRegistry хранит драйверы производителей.
type VendorRegistry struct {
	mu       sync.RWMutex
	adapters map[string]VendorAdapter
}

func NewVendorRegistry() *VendorRegistry {
	return &VendorRegistry{adapters: map[string]VendorAdapter{}}
}

func (r *VendorRegistry) Register(a VendorAdapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[a.Protocol()] = a
}

func (r *VendorRegistry) Get(protocol string) (VendorAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[protocol]
	if !ok {
		return nil, fmt.Errorf("драйвер для протокола '%s' не зарегистрирован", protocol)
	}
	return a, nil
}

func (r *VendorRegistry) All() []VendorAdapter {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]VendorAdapter, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a)
	}
	return out
}

// VendorCollector регулярно опрашивает облака производителей и кладёт данные в мониторинг.
type VendorCollector struct {
	registry  *VendorRegistry
	modems    repositories.ModemRepository
	telemetry *TelemetryService
	log       *zap.Logger
	interval  time.Duration
}

func NewVendorCollector(reg *VendorRegistry, modems repositories.ModemRepository, t *TelemetryService, log *zap.Logger, interval time.Duration) *VendorCollector {
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	return &VendorCollector{registry: reg, modems: modems, telemetry: t, log: log, interval: interval}
}

func (c *VendorCollector) Run(ctx context.Context) {
	c.log.Info("[Collector] Запуск сборщика облачных данных", zap.Duration("interval", c.interval))
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		c.PollAll(ctx)
		select {
		case <-ctx.Done():
			c.log.Info("[Collector] Остановка сборщика")
			return
		case <-ticker.C:
		}
	}
}

// PollAll — один проход по всем драйверам.
func (c *VendorCollector) PollAll(ctx context.Context) {
	for _, a := range c.registry.All() {
		modems, err := c.modems.ListByProtocol(ctx, a.Protocol())
		if err != nil {
			c.log.Error("[Collector] список модемов", zap.String("protocol", a.Protocol()), zap.Error(err))
			continue
		}
		// модемы, которые читает локальный агент, облаком не опрашиваем
		active := modems[:0]
		for _, m := range modems {
			if m.AgentID == nil {
				active = append(active, m)
			}
		}
		if len(active) == 0 {
			continue
		}
		reqCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		payloads, err := a.Collect(reqCtx, active)
		cancel()
		if err != nil {
			c.log.Warn("[Collector] ошибка опроса", zap.String("protocol", a.Protocol()), zap.Error(err))
		}
		for serial, payload := range payloads {
			if _, err := c.telemetry.ProcessModemMessage(ctx, serial, payload); err != nil {
				c.log.Error("[Collector] обработка данных", zap.String("serial", serial), zap.Error(err))
			}
		}
		if len(payloads) > 0 {
			c.log.Debug("[Collector] OK", zap.String("protocol", a.Protocol()), zap.Int("modems", len(payloads)))
		}
	}
}
