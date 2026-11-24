# AtaEnergo

CRM для мониторинга солнечных станций. Go 1.23 (в Docker 1.24), Fiber v2, WebSocket и pgx v5. В Compose работают PostgreSQL 16, Redis 7 и MQTT-брокер Mosquitto 2.

## Где искать код

- `main.go` и `server/` запускают сервер и связывают маршруты с обработчиками. Запрос проходит через `handlers/` → `services/` → `db/repositories/`.
- `config/` — настройки, `middleware/` — авторизация, `types/` — модели, `pkg/` — общие пакеты и интеграции, `cmd/` — утилиты и отдельный воркер.
- `web/src/` — интерфейс на React 19, TypeScript, Vite и Tailwind CSS 4. `agent/` — Python-агент для оборудования на объекте, его запуск описан в `docs/AGENT.md`.
- `db/migrations/` — схема PostgreSQL, `db/seeds/` — SQL для наполнения базы. `docker/` — файлы контейнеров, `docs/` — документация и Swagger.

## Запуск и проверка

Команды выполняй из корня репозитория.

- **Стенд.** Для первого запуска создай `.env` командой `test -f .env || cp .env.example .env` и настрой его по `docs/SETUP.md`. Запусти `docker compose up -d --build`. Проверка при стандартном порте — `curl -fsS http://localhost:8000/api/health`.
- **Go.** При доступной PostgreSQL с применёнными миграциями запускай сервер через `make run`. Тесты — `make test` (`go test ./...`), статический анализ — `make vet`.
- **Интерфейс.** С Node.js 22 установи зависимости через `npm --prefix web ci` и запусти `npm --prefix web run dev`. Vite проксирует API и WebSocket на порт 8000. Проверки — `npm --prefix web run lint` и `npm --prefix web run build` (проверка типов и сборка).

## Правила работы

- Меняй только файлы, необходимые для задачи. Сохраняй чужие правки и стиль соседнего кода. Обновляй зависимости и форматируй посторонние файлы только по запросу.
- Ищи через `rg` и читай только нужные фрагменты. Пропускай `web/node_modules/`, `web/dist/` и сгенерированный Swagger при обзорном поиске. При диагностике начни с `docker compose logs --tail=100 app database`.
- Сохраняй привязку приложения к loopback и доступ к PostgreSQL и Redis внутри сети Compose. Для остановки используй `docker compose down`. В томе `postgres-data-main` хранится БД. Удаление тома (`down -v`) и очистку Docker через `prune` выполняй только по явному запросу.
- При старте контейнер `app` сам применяет миграции. Изменения схемы оформляй новой парой `.up.sql` и `.down.sql`. Перед откатом, `make migrate-force` или удалением данных сверь целевую БД и согласуй операцию. На рабочей базе держи `SEED_DEMO=false` и `DEMO_MODE=false`.
- Используй `.env.example` как справочник. Содержимое `.env` и `agent_config.json`, пароли и токены не включай в коммиты, логи и ответы.
