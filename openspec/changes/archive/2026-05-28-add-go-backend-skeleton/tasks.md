## 1. Repository scaffolding

- [x] 1.1 Create the `backend/` directory at the repository root
- [x] 1.2 Initialize the Go module: `cd backend && go mod init github.com/jiantaoli/openlovart/backend` (use the actual repo path; adjust if the user prefers a different module path)
- [x] 1.3 Add `backend/.gitignore` covering `.env`, `*.log`, and the compiled binary
- [x] 1.4 Create the `cmd/server/`, `internal/config/`, `internal/logging/`, `internal/db/`, `internal/httpserver/`, `internal/httpserver/handlers/`, and `internal/models/` directories with placeholder `.gitkeep` files where empty
- [x] 1.5 Add Go dependencies via `go get`: `github.com/gin-gonic/gin`, `gorm.io/gorm`, `gorm.io/driver/postgres`, `github.com/spf13/viper`
- [x] 1.6 Run `go mod tidy` and commit `go.mod` / `go.sum`

## 2. Configuration (viper)

- [x] 2.1 Define the `Config` struct in `internal/config/config.go` with fields `Env`, `HTTPAddr`, `LogLevel`, `DatabaseURL` and `mapstructure` tags matching env var names
- [x] 2.2 Implement `Load() (*Config, error)` that registers defaults (`ENV=dev`, `HTTP_ADDR=:8080`, `LOG_LEVEL=info`, `DATABASE_URL=postgres://openlovart:openlovart@localhost:5432/openlovart?sslmode=disable`)
- [x] 2.3 Configure viper to read environment variables (`AutomaticEnv`) and to optionally load `backend/.env` if it exists, with environment variables taking precedence
- [x] 2.4 Add a `Validate()` method or inline validation that rejects empty `DATABASE_URL` and unrecognized `LOG_LEVEL` values
- [x] 2.5 Create `backend/.env.example` documenting every variable and its default

## 3. Logging (slog)

- [x] 3.1 Implement `internal/logging/logger.go` exposing `New(cfg *config.Config) *slog.Logger`
- [x] 3.2 Select the slog handler by `cfg.Env`: text handler for `dev`, JSON handler for any other value
- [x] 3.3 Map `cfg.LogLevel` strings (`debug|info|warn|error`) to `slog.Level` values; fall back to `info` on unknown input
- [x] 3.4 Set the returned logger as the process default via `slog.SetDefault`

## 4. Database (GORM)

- [x] 4.1 Implement `internal/db/postgres.go` exposing `Open(ctx context.Context, cfg *config.Config, log *slog.Logger) (*gorm.DB, error)`
- [x] 4.2 Open the connection with `gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{Logger: <slog adapter>})`
- [x] 4.3 Configure the underlying `*sql.DB` pool: `MaxOpenConns=20`, `MaxIdleConns=5`, `ConnMaxLifetime=time.Hour`
- [x] 4.4 Perform an initial `PingContext` with a 5-second timeout; return any error to the caller
- [x] 4.5 Implement a small adapter that satisfies `gorm/logger.Interface` and forwards records to the slog logger, respecting the configured log level
- [x] 4.6 Implement `internal/db/migrate.go` with package-level `RegisterModels(...any)` and `AutoMigrate(*gorm.DB) error`; an empty registry MUST be a no-op that returns `nil`
- [x] 4.7 Implement `Close(db *gorm.DB) error` that retrieves the underlying `*sql.DB` and calls `Close()`

## 5. HTTP server and health endpoint

- [x] 5.1 Implement `internal/httpserver/handlers/health.go` exposing a `Health(db *gorm.DB) gin.HandlerFunc`
- [x] 5.2 Inside the handler, call `sqlDB.PingContext` with a 1-second `context.WithTimeout`; return HTTP 200 `{"status":"ok","db":"ok"}` on success, HTTP 503 `{"status":"degraded","db":"down"}` on failure or timeout
- [x] 5.3 Implement `internal/httpserver/router.go` exposing `New(cfg *config.Config, log *slog.Logger, db *gorm.DB) *gin.Engine`
- [x] 5.4 Register the `/healthz` route on the engine; install a Gin recovery middleware and a request-logging middleware that uses the slog logger
- [x] 5.5 Set Gin mode based on `cfg.Env` (`gin.ReleaseMode` for non-`dev`)

## 6. Entry point and lifecycle

- [x] 6.1 Implement `cmd/server/main.go` orchestrating: `config.Load` → `logging.New` → `db.Open` → `db.AutoMigrate` → build `httpserver.New` → start `http.Server`
- [x] 6.2 Wrap the Gin engine in `&http.Server{Addr: cfg.HTTPAddr, Handler: router, ReadHeaderTimeout: 10s}` and start it in a goroutine
- [x] 6.3 Subscribe to `SIGINT` and `SIGTERM` via `signal.NotifyContext`; on signal, call `server.Shutdown(ctxWith10sTimeout)` then `db.Close`
- [x] 6.4 Exit with status 0 on clean shutdown; exit non-zero on startup failures or shutdown deadline overrun

## 7. Local PostgreSQL via docker-compose

- [x] 7.1 Create `docker-compose.dev.yml` at the repository root with a single `postgres` service using image `postgres:16-alpine`
- [x] 7.2 Set environment: `POSTGRES_USER=openlovart`, `POSTGRES_PASSWORD=openlovart`, `POSTGRES_DB=openlovart`
- [x] 7.3 Map host port `5432:5432` and attach a named volume `openlovart_pgdata` mounted at `/var/lib/postgresql/data`
- [x] 7.4 Add a `healthcheck` clause using `pg_isready -U openlovart` with sensible interval/retries

## 8. Documentation

- [x] 8.1 Create `backend/README.md` describing: prerequisites (Go 1.22+, Docker), how to start PostgreSQL via `docker-compose -f docker-compose.dev.yml up -d`, how to run the backend via `go run ./cmd/server`, configuration variable reference, and how to verify with `curl http://localhost:8080/healthz`
- [x] 8.2 In `backend/README.md`, document AutoMigrate's known limitations (no destructive operations) and the convention that future model packages register their models in `main.go`
- [x] 8.3 Add a top-level `README.md` note (or new section) pointing developers to `backend/README.md` for backend setup; do not modify Supabase/Clerk sections in this phase

## 9. Verification

- [x] 9.1 With `docker-compose -f docker-compose.dev.yml up -d` running, execute `go run ./cmd/server` and confirm a startup log line including the listen address
      _Verified against reused `lobe-postgres` (paradedb/paradedb:latest-pg17, port 5432) with `DATABASE_URL=postgres://postgres:***@localhost:5432/openlovart?sslmode=disable HTTP_ADDR=:8081`. Logs show `backend starting addr=:8081`, `database connected`, `http server listening`._
- [x] 9.2 Run `curl -i http://localhost:8080/healthz` and confirm HTTP 200 with body `{"status":"ok","db":"ok"}`
      _Verified on port 8081: `HTTP/1.1 200 OK` with body `{"db":"ok","status":"ok"}`._
- [x] 9.3 Stop the PostgreSQL container, hit `/healthz` again, and confirm HTTP 503 with body `{"status":"degraded","db":"down"}`
      _Code-reviewed only (`internal/httpserver/handlers/health.go`): both `gormDB.DB()` failure and `sqlDB.PingContext` error/timeout (1s) branches return HTTP 503 with `{"status":"degraded","db":"down"}`. Live DB-down test skipped to avoid disturbing the shared `lobe-postgres` instance._
- [x] 9.4 Send `SIGINT` to the running backend (Ctrl-C) and confirm graceful shutdown logs and exit code 0
      _Verified via `SIGTERM` (equivalent path through `signal.NotifyContext`): logs `shutdown signal received, stopping server` followed by `backend stopped cleanly`; process exited 0._
- [x] 9.5 Confirm `go vet ./...` and `go build ./...` succeed from `backend/`
      _Both commands exited 0 with no output after `go mod tidy`._
