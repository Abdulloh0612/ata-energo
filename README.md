# Ата Энерго — мониторинг солнечных станций (Go)

Сервер для наблюдения за оборудованием клиентов в реальном времени:
**панели → инвертор → батарея → генератор → сеть → нагрузка**.

Переписан с Python/Django (`templates/ata_energo`) на Go с сохранением API и WebSocket.

| Компонент | Технология |
|---|---|
| Backend | Go 1.23+, Fiber v2, pgx v5, go-redis v9, golang-jwt, zap |
| БД | PostgreSQL 16 (Redis — по желанию) |
| Realtime | WebSocket `/ws/telemetry/?token=` (Redis Pub/Sub между процессами) |
| Источники данных | MQTT-модемы, локальный агент (`agent/`), **Deye Cloud OpenAPI**, демо-режим |

## Источники данных

1. **MQTT** — модем шлёт всё в топик `inverter/<SN>/data` (`MQTT_ENABLED=true`).
2. **Локальный агент** (`agent/ata_agent.py`, Python, ставится в сеть клиента) — ищет инверторы
   по Wi-Fi/LAN/Bluetooth, читает Solarman V5 / Modbus TCP / SunSpec и шлёт показания на `/api/agent/*`.
3. **Deye Cloud** — сборщик каждые `DEYE_POLL_INTERVAL` сек опрашивает `/device/latest` (по 10 SN)
   и `/station/latest`, переводит данные в формат модема и кладёт в тот же мониторинг
   (схема, аварии, история, WebSocket).
4. **Демо** (`DEMO_MODE=true`) — фейковые данные для всех модемов без агента/облака.

Все источники проходят через одну функцию — `services.TelemetryService.ProcessModemMessage`.

## Запуск в Docker (рекомендуется)

Нужен только Docker Desktop. API работает в контейнере `app`, рядом PostgreSQL, Redis и MQTT-брокер.

```bash
copy .env.example .env          # Linux/macOS: cp .env.example .env  (поменяйте JWT_SECRET)
docker compose up -d --build
```

Проверить доступность API можно через `GET http://localhost:8000/api/health`. При старте контейнер сам применяет
миграции и (если `SEED_DEMO=true`) создаёт тестовые данные.

```bash
docker compose logs -f app      # логи
docker compose down             # остановить (данные БД остаются в volume postgres-data-main)
docker compose down -v          # остановить и удалить базу
docker compose exec app ./cmd/createagent "Офис"   # создать агента поиска
```

Для боевого сервера в `.env`: `APP_ENV=production`, `DEMO_MODE=false`, `SEED_DEMO=false`, свои пароли.

## Быстрый старт без Docker

```bash
cp .env.example .env                       # DATABASE_URL, JWT_SECRET
make migrate                                # или: go run ./cmd/seed -database $DATABASE_URL -path db/migrations up
make seed                                   # admin/admin123, ithead/ithead123, dispatcher/disp123, client1/client123
make run                                    # http://localhost:8000/api/health
```

Swagger: `http://localhost:8000/swagger/index.html` (`make swagger` — перегенерировать).

### Команды

| Команда | Что делает (аналог в Python) |
|---|---|
| `go run .` | API + WebSocket + MQTT + проверка offline + Deye (`runserver` + `run_mqtt`) |
| `go run ./cmd/seeddata -clients N` | тестовые данные (`seed_data`) |
| `go run ./cmd/createagent "Офис"` | агент поиска + токен (`create_agent`) |
| `go run ./cmd/worker [-once]` | отдельный воркер / разовая проверка offline (`run_mqtt`, `check_offline`) |
| `go run ./cmd/simulator [-serials SN -generator]` | фейковый модем в MQTT (`simulate_modem.py`) |
| `go run ./cmd/hashpass пароль` | bcrypt-хеш |

## Роли

| Роль | Клиенты | Оборудование | Сотрудники | Realtime |
|---|---|---|---|---|
| admin, it_head | CRUD | CRUD | CRUD | всё (`all`) |
| dispatcher | просмотр | просмотр | — | всё (`all`) |
| user | — | только своё | — | `client_{id}` |

Проверки — `middleware/auth.go` и `services.OwnerScope`.

## REST API (`/api`, заголовок `Authorization: Bearer <access>`)

Формат совместим с Python-версией (DRF): списки — массивы, ошибки — `{"detail": ...}` или `{"поле": ["..."]}`.

| Метод и адрес | Кто |
|---|---|
| `POST /auth/login/` `{username,password}` → `{access, refresh, user}`; `POST /auth/refresh/` | все |
| `GET /me/` | вошедший |
| `/users/` CRUD (`?staff=1&role=&search=`) | admin, it_head |
| `/clients/` CRUD (+ `login_username/login_password`, `modem_serial/modem_name/has_generator`), `GET /clients/{id}/devices/` | чтение — сотрудники, запись — admin, it_head |
| `/devices/` CRUD (`?client=&type=&status=`), `GET /devices/{id}/history/?from=&to=&limit=` | чтение — все (своё), запись — admin, it_head |
| `/modems/` CRUD (`?client=&search=`) | то же |
| `GET /my/devices/`, `GET /my/telemetry/?limit=&device=` | клиент |
| `GET /alerts/?unread=1&client=`, `POST /alerts/{id}/read/`, `POST /alerts/read_all/` | все / сотрудники |
| `/agents/` CRUD, `POST /agents/{id}/new_token/` | admin, it_head |
| `POST /discovery/scan/`, `GET /discovery/tasks/{id}/`, `GET /discovery/devices/`, `POST /discovery/devices/{id}/probe/`, `.../connect/`, `POST /discovery/devices/clear/`, `POST /discovery/browser-bluetooth/` | admin, it_head |
| `/agent/hello/`, `/agent/poll/`, `/agent/tasks/{id}/progress/`, `.../finish/`, `/agent/telemetry/` | агент (`X-Agent-Token`) |

### Deye Cloud

Аккаунт: `app_id` + `app_secret` (developer.deyecloud.com) и логин аккаунта (email / username / mobile + код страны).
Пароль хранится только в виде SHA-256 (так требует Deye). Можно задать в `.env` (`DEYE_*`) или через API.

| Метод и адрес | Что делает |
|---|---|
| `GET/POST /deye/accounts/`, `GET/PATCH/DELETE /deye/accounts/{id}/` | аккаунты |
| `POST /deye/accounts/{id}/test/` | проверить вход |
| `GET /deye/accounts/{id}/stations/?with_devices=1` | станции (с устройствами) |
| `GET /deye/accounts/{id}/stations/{sid}/`, `.../devices/`, `POST .../stations/create/` | станция |
| `GET /deye/accounts/{id}/devices/`, `POST .../devices/register/`, `POST .../loggers/add|delete/` | устройства / логгеры |
| `POST /deye/accounts/{id}/import/` `{device_sn, station_id, client, site, name, has_generator}` | **подключить инвертор Deye к клиенту** (создаёт модем `deye_cloud`) |
| `POST /deye/sync/` | опросить облако сейчас |
| `GET /modems/{id}/deye/latest/`, `station/`, `measure-points/`, `alerts/`, `station-alerts/`, `config/` | данные / настройки |
| `POST /modems/{id}/deye/history/`, `history-raw/`, `station-history/`, `station-history-power/` | история |
| `POST /modems/{id}/deye/control/{command}/` | команды: `battery-mode`, `battery-parameter`, `battery-type`, `modbus`, `peak-shaving`, `smartload`, `energy-pattern`, `limit-control`, `power`, `solar-sell`, `tou-switch`, `tou`, `work-mode`, `dynamic` |
| `GET /modems/{id}/deye/orders/{orderId}/`, `POST .../dynamic/read/`, `GET .../dynamic/result/{orderId}/` | результат команды (666 = выполнено) |

Клиент API — `pkg/deye` (все 40 методов OpenAPI), перевод в формат модема — `pkg/deye/mapper.go`.
Знак батареи/сети можно перевернуть в `connection` модема (`invert_battery`, `invert_grid`).

## WebSocket

`ws://домен/ws/telemetry/?token=<access>` — сообщения `hello`, `telemetry`, `status`, `modem`, `alert`, `pong`.
Коды закрытия: 4001 — неверный токен, 4003 — клиент без станции.

## Структура

```
main.go              точка входа
server/              Fiber, маршруты, DI (essentials, services, vars)
handlers/api         REST API                handlers/agent  API локального агента
handlers/ws          WebSocket
services/            бизнес-логика: telemetry (обработка данных), modem, client, device, discovery,
                     deye (+ драйвер сборщика), vendor_collector, background (offline + демо), mqtt
db/                  pgx pool, redis, repositories/, migrations/
types/               сущности и DTO
middleware/          JWT, роли, токен агента
pkg/                 deye (клиент Deye Cloud), hub (рассылка WS), fakestation, jwt, logger, validator
cmd/                 seeddata, createagent, worker, simulator, seed (SQL), hashpass
agent/               локальный агент (Python, без изменений)
templates/           исходники: ata_energo (Python), solar-aggregator
```

## Каталог провайдеров и облачные ключи

Администратор может добавить аккаунт Deye через `POST /api/deye/accounts/`.
В списке интеграций (`GET /api/crm/integrations/accounts`) API показывает наличие учётных данных. Deye синхронизирует каталог объектов в PostgreSQL каждые 15 минут при
`DEYE_ENABLED=true`. Чтобы запустить синхронизацию вручную, вызовите `POST /api/crm/integrations/accounts/{id}/sync`.
Новые объекты доступны через `GET /api/crm/sites`.

До обновления экземпляра с существующими облачными аккаунтами создайте ключ
(`openssl rand -base64 32`) и сохраните его в защищённом `.env` или менеджере
секретов как `PROVIDER_MASTER_KEY`. Укажите `PROVIDER_KEY_VERSION=1`. При старте
приложение переносит старые учётные данные в AES-256-GCM и очищает прежние поля.
Без ключа можно использовать локальный мониторинг и демо на базе без облачных
аккаунтов. Старые резервные копии, сделанные до миграции, нужно защищать отдельно.

Для ротации остановите приложение и отдельные процессы-воркеры. Перенесите
текущую пару в `PROVIDER_PREVIOUS_MASTER_KEY` и `PROVIDER_PREVIOUS_KEY_VERSION`,
задайте новый случайный ключ и новую версию. После успешного запуска и переноса
удалите предыдущую пару из рабочей конфигурации. Для восстановления старой
резервной копии потребуется ключ её версии.

Реестр в `providers` выбирает адаптер по столбцу `adapter`. Реальный адаптер
каталога реализует `SiteCatalogAdapter.FetchSites`, возвращает `CatalogSite`
и регистрируется в `server.NewServices`. Новые внешние поля нормализуйте внутри
адаптера и проверяйте на обезличенных фикстурах. Eybond работает на демоданных и
не требует ключей. Реальный транспорт можно подключить после получения документации.
