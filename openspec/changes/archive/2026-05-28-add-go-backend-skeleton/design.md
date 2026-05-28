## Context

OpenLovart's frontend currently talks directly to Supabase (PostgREST) using a Clerk-issued JWT, with row-level security as the only authorization boundary. We are migrating to a Go backend that owns all PostgreSQL access, with our own authentication system. Phase 1 (this change) lays down the backend foundation only; authentication and business APIs are scoped to subsequent changes.

Tooling decisions agreed upstream (binding for this and follow-up phases):

- Web framework: **Gin**
- ORM: **GORM v2** (`gorm.io/gorm` + `gorm.io/driver/postgres`)
- Migration mechanism: **GORM AutoMigrate** (no separate SQL migration tool)
- Configuration: **viper** (env-first, with optional `backend/.env` for local dev)
- Logging: **`log/slog`** (standard library) — no extra dependency
- Browser ↔ backend: **direct connection** (CORS will be configured in Phase 2 when auth lands; not needed yet for `/healthz`)
- DSN default for local dev: `postgres://openlovart:openlovart@localhost:5432/openlovart`

The PostgreSQL instance is self-managed by the user. For local development, a `docker-compose.dev.yml` will start a PostgreSQL container that matches the default DSN.

## Goals / Non-Goals

**Goals:**

- Provide a runnable Go service (`go run ./cmd/server`) that starts, connects to PostgreSQL, runs AutoMigrate, serves HTTP, and shuts down gracefully.
- Establish a clear, conventional package layout that Phase 2 (auth) and Phase 3 (business APIs) can extend without restructuring.
- Make local development frictionless: one command for the database (`docker-compose -f docker-compose.dev.yml up -d`), one command for the backend (`go run ./cmd/server`).
- Centralize configuration loading so every later subsystem reads from one typed `Config` struct.
- Expose `GET /healthz` for liveness checks and as a smoke-test surface for the rest of the stack.

**Non-Goals:**

- **No authentication, no authorization, no JWT, no cookies, no OIDC.** All auth lives in Phase 2 (`add-auth-with-password-and-oidc`).
- **No business endpoints** (`/api/projects`, `/api/credits`, etc.). Phase 3 (`migrate-business-apis-to-go`).
- **No Dockerfile for the backend itself**, no full-stack docker-compose. Phase 4 (optional).
- **No frontend changes.** `src/` is untouched.
- **No removal of Supabase or Clerk.** They keep working in parallel until Phase 3 completes.
- **No production deployment story** (TLS, reverse proxy, secrets management). Local dev only.
- **No CI/CD pipeline changes.**
- **No models registered with AutoMigrate yet.** AutoMigrate is wired with an empty model list; Phase 2 and 3 add their own models.

## Decisions

### D1. Directory layout: `backend/` with `cmd/` + `internal/`

```
backend/
├── cmd/
│   └── server/
│       └── main.go              # Entry point
├── internal/
│   ├── config/
│   │   └── config.go            # viper loader → typed Config
│   ├── logging/
│   │   └── logger.go            # slog setup (level, JSON in prod, text in dev)
│   ├── db/
│   │   ├── postgres.go          # GORM open + connection pool
│   │   └── migrate.go           # AutoMigrate runner (model registry)
│   ├── httpserver/
│   │   ├── router.go            # gin engine + routes
│   │   └── handlers/
│   │       └── health.go        # GET /healthz
│   └── models/                  # (empty in Phase 1; populated later)
├── .env.example
├── go.mod
└── README.md
```

**Rationale**: Standard Go layout. `internal/` prevents external imports of backend internals. `cmd/server` lets us add other commands later (e.g., `cmd/migrate` if we ever move off AutoMigrate). Phase 2/3 will add `internal/auth/`, `internal/repository/`, `internal/service/` without churning what's already there.

**Alternatives considered:**
- Flat layout (everything in `backend/`): rejected — won't scale to Phases 2–3.
- Hexagonal/clean architecture with `domain/`, `application/`, `infrastructure/` layers: rejected as over-engineering for a single-service app.

### D2. Configuration: viper, env-first, with `backend/.env` for local

A single `Config` struct populated by viper. Environment variables take precedence over `.env`. `.env` is gitignored.

```go
type Config struct {
    Env        string `mapstructure:"ENV"`         // "dev" | "prod"
    HTTPAddr   string `mapstructure:"HTTP_ADDR"`   // ":8080"
    LogLevel   string `mapstructure:"LOG_LEVEL"`   // "debug" | "info" | "warn" | "error"
    DatabaseURL string `mapstructure:"DATABASE_URL"`
}
```

Defaults: `ENV=dev`, `HTTP_ADDR=:8080`, `LOG_LEVEL=info`, `DATABASE_URL=postgres://openlovart:openlovart@localhost:5432/openlovart?sslmode=disable`.

**Rationale**: viper handles env + file + defaults uniformly. Centralizing in one struct prevents `os.Getenv` sprinkled across the codebase. `mapstructure` tags align viper's binding model with struct fields.

**Alternatives considered:**
- `caarlos0/env`: simpler, but the user explicitly chose viper for richer file/format support down the road.
- Raw `os.Getenv`: rejected — no defaults, no type coercion, no centralization.

### D3. Database driver: GORM with `gorm.io/driver/postgres` (pgx under the hood)

```go
db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{
    Logger: gormSlogAdapter(cfg.LogLevel),
})
sqlDB, _ := db.DB()
sqlDB.SetMaxOpenConns(20)
sqlDB.SetMaxIdleConns(5)
sqlDB.SetConnMaxLifetime(time.Hour)
```

**Rationale**: GORM's official Postgres driver wraps `pgx`, which is the de facto Go driver. Connection pool sizing is reasonable for single-instance dev; Phase 4 may revisit for prod.

### D4. Migrations: AutoMigrate at startup, with a registry

A single `internal/db/migrate.go` exposes:

```go
var registeredModels []any  // package-level, populated by init() in each model package

func RegisterModels(models ...any) { registeredModels = append(registeredModels, models...) }

func AutoMigrate(db *gorm.DB) error {
    if len(registeredModels) == 0 { return nil }  // Phase 1: no-op
    return db.AutoMigrate(registeredModels...)
}
```

Phase 2/3 add models by calling `db.RegisterModels(&User{}, &Project{}, ...)` from each model package's `init()` (or explicitly from `main.go` to keep dependency direction explicit — see open question O1).

**Rationale**: AutoMigrate was chosen by the user. A registry keeps migration concerns out of `main.go` and avoids a giant import block.

**Trade-off accepted**: AutoMigrate cannot drop columns, rename, or perform data backfills. We accept this for now; if the schema ever needs destructive changes, we'll write a one-off migration script or revisit tooling.

### D5. Logging: `log/slog` with environment-aware handler

- `ENV=dev` → text handler, human-readable.
- `ENV=prod` → JSON handler.
- Level controlled by `LOG_LEVEL`.
- A small adapter implements `gorm/logger.Interface` so GORM logs flow through slog.

**Rationale**: Standard library only. No extra dependency. JSON in prod plays nicely with log aggregators later.

### D6. HTTP server lifecycle: graceful shutdown via signal handling

`main.go` runs the Gin engine inside `http.Server`, listens for `SIGINT`/`SIGTERM`, and shuts down with a 10s timeout. On shutdown, the GORM connection pool is closed via `sqlDB.Close()`.

**Rationale**: Standard hygiene. Prevents dropped requests and leaked connections during dev iteration and future container restarts.

### D7. `/healthz` semantics

`GET /healthz` returns:

- `200 OK` with body `{"status":"ok","db":"ok"}` when the server is up **and** `sqlDB.PingContext` succeeds within 1s.
- `503 Service Unavailable` with `{"status":"degraded","db":"down"}` when DB ping fails.

**Rationale**: A health probe that ignores DB connectivity gives false confidence. The 1s timeout prevents the probe from hanging in failure scenarios.

### D8. `docker-compose.dev.yml` scope

Lives at the **repo root** (so it's discoverable alongside the frontend). Contains a single `postgres` service:

- Image: `postgres:16-alpine`
- Env: `POSTGRES_USER=openlovart`, `POSTGRES_PASSWORD=openlovart`, `POSTGRES_DB=openlovart`
- Port: `5432:5432`
- Volume: named volume `openlovart_pgdata` for persistence across restarts.

The backend itself is **not** containerized in this phase; developers run `go run ./cmd/server` on the host.

**Rationale**: Smallest possible compose footprint that matches the chosen DSN. Containerizing the backend now would add Dockerfile + reload tooling (air/CompileDaemon) without yielding Phase-1 value.

## Risks / Trade-offs

- **AutoMigrate's destructive limitations** → Accepted for now; we have no real data to lose. Document the limitation in `backend/README.md` so future contributors know what AutoMigrate will not do.
- **Empty model registry in Phase 1 means AutoMigrate is a no-op** → That is intentional and correct. We verify the wiring by ensuring the function runs without error against an empty database; Phase 2 will be the first real migration test.
- **Port 8080 might collide with the user's existing services** → Make `HTTP_ADDR` configurable via env. Document override in README.
- **PostgreSQL version mismatch (compose uses 16, user's PG might differ)** → AutoMigrate uses portable SQL; should be fine on PG 13+. README will note minimum supported version (PG 13).
- **`sslmode=disable` in default DSN** → Acceptable for local dev only. README must explicitly flag that production DSN should use `sslmode=require` or stronger; we'll enforce in Phase 4.
- **Single-process pool sizing (20 max, 5 idle)** → Sane defaults for single-user dev. Will revisit in Phase 4.
- **No CI smoke test added** → Out of scope for Phase 1; the user can run the server locally to validate.

## Migration Plan

This change is **additive** and **non-breaking**:

1. Land the `backend/` directory and `docker-compose.dev.yml` with no wiring to the existing frontend.
2. Verify locally:
   - `docker-compose -f docker-compose.dev.yml up -d` brings up Postgres.
   - `cd backend && go mod tidy && go run ./cmd/server` starts the server.
   - `curl http://localhost:8080/healthz` returns `{"status":"ok","db":"ok"}`.
3. Stopping the backend cleanly (Ctrl-C) results in graceful shutdown logs.

**Rollback**: Delete `backend/` and `docker-compose.dev.yml`. No production state, no data, no consumer code references this work yet.

## Open Questions

- **O1. Model registration style**: register via `init()` in each model package, or explicitly from `main.go`?
  - _Leaning toward explicit registration in `main.go`_ to keep dependency direction obvious and to make it impossible to forget a model when reading the entry point. Decide when Phase 2 needs the first model.
- **O2. Should the backend ship a `Makefile` for `run` / `migrate` / `tidy` shortcuts?**
  - Defer. Not needed in Phase 1. Add when Phase 2 introduces enough commands to justify it.
- **O3. Where does `backend/.env` get sourced from for `go run`?**
  - viper supports loading a file directly. We'll wire it to read `backend/.env` if present, with env vars overriding. Documented in README.
