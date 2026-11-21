// Package hub — рассылка сообщений в WebSocket по группам.
//
// Группы:
//
//	"all"         — сотрудники (admin, it_head, dispatcher) видят данные всех клиентов
//	"client_{id}" — клиент видит только свои данные
//
// Если передан Redis — сообщения идут через Redis Pub/Sub, поэтому их видят
// все процессы (сайт, отдельный MQTT-воркер). Без Redis — только внутри процесса.
package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const redisChannel = "ata_energo:events"

// Subscriber — одно подключение (браузер).
type Subscriber struct {
	C      chan []byte
	groups []string
}

type envelope struct {
	Groups  []string        `json:"groups"`
	Payload json.RawMessage `json:"payload"`
}

type Hub struct {
	mu     sync.RWMutex
	groups map[string]map[*Subscriber]struct{}
	redis  *redis.Client
	log    *zap.Logger
}

func New(rdb *redis.Client, log *zap.Logger) *Hub {
	return &Hub{groups: map[string]map[*Subscriber]struct{}{}, redis: rdb, log: log}
}

// ClientGroup — имя группы клиента.
func ClientGroup(clientID int64) string {
	return fmt.Sprintf("client_%d", clientID)
}

// Subscribe подписывает новое подключение на группы.
func (h *Hub) Subscribe(groups []string) *Subscriber {
	s := &Subscriber{C: make(chan []byte, 256), groups: groups}
	h.mu.Lock()
	for _, g := range groups {
		if h.groups[g] == nil {
			h.groups[g] = map[*Subscriber]struct{}{}
		}
		h.groups[g][s] = struct{}{}
	}
	h.mu.Unlock()
	return s
}

// Unsubscribe отписывает подключение от всех групп.
func (h *Hub) Unsubscribe(s *Subscriber) {
	h.mu.Lock()
	for _, g := range s.groups {
		delete(h.groups[g], s)
		if len(h.groups[g]) == 0 {
			delete(h.groups, g)
		}
	}
	h.mu.Unlock()
}

// Broadcast отправляет сообщение группе "all" и группе "client_{id}" (если clientID > 0).
func (h *Hub) Broadcast(clientID int64, message any) {
	payload, err := json.Marshal(message)
	if err != nil {
		h.log.Error("hub: marshal message", zap.Error(err))
		return
	}
	groups := []string{"all"}
	if clientID > 0 {
		groups = append(groups, ClientGroup(clientID))
	}
	if h.redis != nil {
		data, _ := json.Marshal(envelope{Groups: groups, Payload: payload})
		if err := h.redis.Publish(context.Background(), redisChannel, data).Err(); err == nil {
			return
		} else {
			// Redis недоступен — не роняем обработку, отдаём хотя бы локальным подписчикам
			h.log.Warn("hub: redis publish failed, delivering locally", zap.Error(err))
		}
	}
	h.deliver(groups, payload)
}

// Run слушает Redis (если он есть) и раздаёт сообщения локальным подписчикам. Блокирует до ctx.Done().
func (h *Hub) Run(ctx context.Context) {
	if h.redis == nil {
		<-ctx.Done()
		return
	}
	sub := h.redis.Subscribe(ctx, redisChannel)
	defer sub.Close()
	ch := sub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var env envelope
			if err := json.Unmarshal([]byte(msg.Payload), &env); err != nil {
				continue
			}
			h.deliver(env.Groups, env.Payload)
		}
	}
}

func (h *Hub) deliver(groups []string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	seen := map[*Subscriber]struct{}{}
	for _, g := range groups {
		for s := range h.groups[g] {
			if _, dup := seen[s]; dup {
				continue
			}
			seen[s] = struct{}{}
			select {
			case s.C <- payload:
			default: // медленный браузер — пропускаем сообщение, не блокируем рассылку
			}
		}
	}
}
