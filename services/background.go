package services

import (
	"context"
	"time"

	"ata-energo/db/repositories"
	"ata-energo/pkg/fakestation"

	"go.uber.org/zap"
)

// BackgroundService — фоновая задача внутри веб-сервера.
//
//  1. Каждые несколько секунд проверяет устройства без связи (offline + Alert).
//  2. Если DEMO_MODE=true — сама генерирует фейковые данные для ВСЕХ модемов из базы
//     (кроме подключённых через агента и облако — они настоящие).
//     Так сайт можно проверить без MQTT и без Redis.
type BackgroundService struct {
	telemetry *TelemetryService
	modems    repositories.ModemRepository
	log       *zap.Logger
	demo      bool
	interval  time.Duration
}

func NewBackgroundService(t *TelemetryService, modems repositories.ModemRepository, log *zap.Logger, demo bool, interval time.Duration) *BackgroundService {
	if interval < time.Second {
		interval = time.Second
	}
	return &BackgroundService{telemetry: t, modems: modems, log: log, demo: demo, interval: interval}
}

// Run — бесконечный цикл фоновой задачи (до ctx.Done()).
func (b *BackgroundService) Run(ctx context.Context) {
	b.log.Info("Фоновая задача запущена", zap.Bool("demo_mode", b.demo), zap.Duration("interval", b.interval))
	stations := map[int64]*fakestation.Station{}
	ticker := time.NewTicker(b.interval)
	defer ticker.Stop()
	tick := 0
	for {
		if b.demo {
			if err := b.demoTick(ctx, stations); err != nil {
				b.log.Error("Ошибка демо-режима", zap.Error(err))
			}
		}
		if tick%3 == 0 {
			if n, err := b.telemetry.CheckOfflineDevices(ctx); err != nil {
				b.log.Error("Ошибка проверки offline", zap.Error(err))
			} else if n > 0 {
				b.log.Info("Стали offline", zap.Int("count", n))
			}
		}
		tick++
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// demoTick — каждый модем из базы "присылает" сообщение со всеми данными.
func (b *BackgroundService) demoTick(ctx context.Context, stations map[int64]*fakestation.Station) error {
	list, err := b.modems.ListForDemo(ctx)
	if err != nil {
		return err
	}
	for i := range list {
		m := &list[i]
		st := stations[m.ID]
		if st == nil || st.HasGenerator != m.HasGenerator {
			st = fakestation.New(m.HasGenerator, 2)
			stations[m.ID] = st
		}
		if _, err := b.telemetry.ProcessModemMessage(ctx, m.Serial, st.ModemPayload(m.Serial, b.interval.Seconds())); err != nil {
			return err
		}
	}
	return nil
}
