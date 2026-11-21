FROM golang:1.24 AS local

WORKDIR /usr/src/app

RUN go install github.com/cosmtrek/air@v1.51.0 && \
    go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.0

COPY go.mod go.sum ./
RUN go mod download

RUN apt-get update && apt-get install -y sshpass

COPY . .

CMD ["air"]

FROM golang:1.24 AS builder

WORKDIR /usr/src/app

# Install migrate tool
RUN CGO_ENABLED=0 GOOS=linux go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.17.0

# Copy and download dependencies first for better layer caching
COPY go.mod go.sum ./
RUN go mod download

# Create necessary directories
RUN mkdir -p /usr/src/app/tmp/bin

# Copy the rest of the source code
COPY . .

# Build server executable
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /usr/src/app/tmp/server_process ./main.go

# Build command executables
RUN CGO_ENABLED=0 GOOS=linux go build -o /usr/src/app/tmp/bin/ ./cmd/seed ./cmd/seeddata ./cmd/createagent ./cmd/worker ./cmd/hashpass

FROM node:22-alpine AS frontend
WORKDIR /src
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run check && npm run build

FROM alpine:3.19 AS production

WORKDIR /usr/bin/app

# Install required packages and create directories in a single layer
RUN apk add --no-cache bash curl tzdata && \
    mkdir -p /usr/bin/app/cmd/logger

# Настройки приходят из docker-compose (env_file: .env + environment), .env в образ не кладём
COPY docker/app/health-check.sh /usr/bin/app/health-check.sh
COPY docker/app/entrypoint.sh /usr/bin/app/entrypoint.sh
# на случай Windows-переводов строк (CRLF)
RUN sed -i 's/\r$//' /usr/bin/app/entrypoint.sh /usr/bin/app/health-check.sh && \
    chmod +x /usr/bin/app/entrypoint.sh /usr/bin/app/health-check.sh

# Сайт (index.html + static) и SQL-миграции
COPY web /usr/bin/app/web
COPY --from=frontend /src/static/build /usr/bin/app/web/static/build
COPY db/migrations /usr/bin/app/db/migrations

# Copy server executable and migrate tool
COPY --from=builder --chmod=755 /usr/src/app/tmp/server_process /usr/bin/app/server_process
COPY --from=builder --chmod=755 /go/bin/migrate /usr/bin/migrate

# Copy command executables
COPY --from=builder --chmod=755 /usr/src/app/tmp/bin/ /usr/bin/app/cmd/

ENV WEB_DIR=/usr/bin/app/web \
    SERVER_PORT=8000

EXPOSE 8000

HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 \
    CMD curl -sf "http://localhost:${SERVER_PORT}/api/health" || exit 1

CMD ["/usr/bin/app/entrypoint.sh"]
