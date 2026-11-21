.PHONY: run build test vet migrate migrate-down migrate-force migrate-create seed seed-sql seed-down agent worker simulator swagger swagger-fmt help

# Database URL
POSTGRESQL_URL := "postgresql://ata:ata@ata-energo-database:5432/ata_energo?sslmode=disable"

# Directories
MIGRATION_DIR := db/migrations
SEED_DIR := db/seeds

# ==================== App ====================

run:
	go run .

build:
	go build -o tmp/server_process .

test:
	go test ./...

vet:
	go vet ./...

# Отдельный воркер: MQTT + проверка offline + Deye Cloud (обычно не нужен — всё это делает сервер)
worker:
	go run ./cmd/worker

# Симулятор модемов в MQTT (как simulate_modem.py)
simulator:
	go run ./cmd/simulator

# Создать локального агента: make agent NAME="Офис"
agent:
ifndef NAME
	$(error NAME is required. Usage: make agent NAME="Офис")
endif
	go run ./cmd/createagent -server $${APP_BASE_URL:-http://127.0.0.1:8000} "$(NAME)"

# ==================== Database Management ====================

migrate:
	migrate -database $(POSTGRESQL_URL) -path $(MIGRATION_DIR) up

migrate-down:
	migrate -database $(POSTGRESQL_URL) -path $(MIGRATION_DIR) down 1

migrate-force:
	migrate -database $(POSTGRESQL_URL) -path $(MIGRATION_DIR) force 1

migrate-create:
ifndef NAME
	$(error NAME is required. Usage: make migrate-create NAME=<migration_name>)
endif
	migrate create -ext sql -dir $(MIGRATION_DIR) -seq $(NAME)

# Тестовые данные (admin/admin123, ithead/ithead123, dispatcher/disp123, client1..N/client123 + модемы)
seed:
	go run ./cmd/seeddata -clients 2

# Свои SQL-сиды из db/seeds (если есть)
seed-sql:
	go run cmd/seed/main.go -database $(POSTGRESQL_URL) -path $(SEED_DIR) up

seed-down:
	go run cmd/seed/main.go -database $(POSTGRESQL_URL) -path $(SEED_DIR) down

# ==================== Swagger ====================

swagger:
	@echo "Generating Swagger documentation..."
	@go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g server/server.go -o docs --parseDependencyLevel 1 --parseInternal --parseFuncBody --exclude ./templates,./docker,./agent,./web
	@echo "Done: $${APP_BASE_URL:-http://localhost:8000}/swagger/index.html"

swagger-fmt:
	@echo "Formatting Swagger comments..."
	@go run github.com/swaggo/swag/cmd/swag@v1.16.6 fmt

# ==================== Help ====================

help:
	@echo "Available commands:"
	@echo ""
	@echo "App:"
	@echo "  make run                  - Run server (go run .)"
	@echo "  make build / test / vet   - Build, tests, vet"
	@echo "  make worker               - Separate worker (MQTT + offline + Deye)"
	@echo "  make simulator            - Fake modems -> MQTT"
	@echo "  make agent NAME=\"Офис\"    - Create local discovery agent (prints token)"
	@echo ""
	@echo "Database:"
	@echo "  make migrate              - Run migrations"
	@echo "  make migrate-down         - Rollback last migration"
	@echo "  make migrate-create NAME= - Create new migration"
	@echo "  make seed                 - Test users, clients, modems (DEV only)"
	@echo ""
	@echo "Swagger:"
	@echo "  make swagger              - Generate Swagger docs"
	@echo "  make swagger-fmt          - Format Swagger comments"
