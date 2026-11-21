#!/bin/sh
# Запуск контейнера: миграции -> (тестовые данные) -> сервер (API + сайт + WebSocket).
set -e

cd /usr/bin/app

echo "Applying migrations..."
# База может ещё подниматься — пробуем, пока не получится
until migrate -database "$DATABASE_URL" -path /usr/bin/app/db/migrations up; do
  echo "Database not ready, retry in 2s..."
  sleep 2
done

if [ "${SEED_DEMO:-false}" = "true" ]; then
  echo "Seeding demo data (повторный запуск дубликатов не создаёт)..."
  /usr/bin/app/cmd/seeddata -clients "${SEED_CLIENTS:-2}"
fi

echo "Starting server..."
exec /usr/bin/app/server_process
