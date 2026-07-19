# Vaha AI — Backend

Go + Fiber API for the Vaha AI tutor app, using **GORM (PostgreSQL)**, **Redis**
and **RabbitMQ**.

## Stack

| Concern        | Choice                          |
| -------------- | ------------------------------- |
| Language       | Go 1.26                         |
| HTTP framework | [Fiber v2](https://gofiber.io)  |
| ORM / DB       | GORM + PostgreSQL               |
| Cache / OTP    | Redis (`go-redis/v9`)           |
| Message queue  | RabbitMQ (`amqp091-go`)         |

## Layout

```
cmd/api/            main entrypoint (wire deps, start/stop)
internal/
  config/           env-based configuration
  database/         MySQL + GORM connection
  cache/            Redis client
  queue/            RabbitMQ connection + publish helper
  server/           Fiber app, middleware, route registration
  handler/          HTTP handlers (controllers)
  model/            GORM models (added per feature)
pkg/logger/         small leveled logger
```

## Prerequisites (local dev)

All three services must be running locally:

- **PostgreSQL** on `127.0.0.1:5432` with a `vaha_ai` database (default user
  `postgres` / password `postgres`)
- **Redis** on `127.0.0.1:6379`
- **RabbitMQ** on `127.0.0.1:5672` (default `guest:guest`)

Create the database once:

```sql
CREATE DATABASE vaha_ai;
```

## Configuration

Copy `.env.example` to `.env` and adjust as needed. Defaults already match a
standard local XAMPP MySQL / local Redis / RabbitMQ setup.

## Run

```bash
go mod tidy      # first time — download dependencies
go run ./cmd/api
```

Then check health:

```bash
curl http://localhost:8080/health
# { "success": true, "status": "ok", "services": { "postgres":"up","redis":"up","rabbitmq":"up" } }
```

## Build

```bash
go build -o bin/api ./cmd/api
```
