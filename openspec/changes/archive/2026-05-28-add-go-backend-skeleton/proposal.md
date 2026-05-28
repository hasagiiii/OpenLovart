## Why

The project currently relies on Supabase (PostgREST + RLS) and Clerk for data access and authentication directly from the browser. We are migrating to a self-hosted architecture where a Go backend mediates all database access against our own PostgreSQL instance, and where authentication is implemented in-house. This first phase establishes the backend foundation — a runnable Gin service with database connectivity and a deployable shape — so that subsequent phases (auth, business APIs) have a stable place to land.

Without this skeleton, the auth and business-API migrations would have nowhere to live and would mix infrastructure concerns with feature work.

## What Changes

- Introduce a new top-level `backend/` directory containing a Go module (Gin-based HTTP server).
- Provide a runnable server exposing `GET /healthz` for liveness checks.
- Establish PostgreSQL connectivity via GORM (`gorm.io/driver/postgres`) using a configurable `DATABASE_URL`.
- Use **GORM AutoMigrate** as the migration mechanism for the project (per decision); Phase 1 wires the AutoMigrate runner with an empty model set so later phases can register models.
- Configuration via **viper** (env + optional `backend/.env`); structured logging via standard library `log/slog`.
- Provide a `docker-compose.dev.yml` that starts a local PostgreSQL instance for development (database `openlovart`, user `openlovart`, password `openlovart`).
- Document local development workflow (run PG via compose, run backend via `go run`, run frontend via `npm run dev`) in `backend/README.md`.
- No frontend changes in this phase. No authentication, no business endpoints. **Supabase and Clerk remain untouched in this phase.**

This change is **non-breaking** to the existing application: the backend runs on a separate port and is not yet wired into the frontend.

## Capabilities

### New Capabilities

- `backend-runtime`: The Go backend service runtime — process startup, configuration loading, structured logging, HTTP server lifecycle, graceful shutdown, and the health endpoint.
- `backend-persistence`: Database connectivity and schema management — PostgreSQL connection pool via GORM, AutoMigrate execution at startup, and the model registry conventions that later phases will extend.

### Modified Capabilities

_None._ This is greenfield infrastructure; no existing OpenSpec capability is altered.

## Impact

- **New code**: `backend/` directory (Go module, Gin server, GORM setup, config, logging, healthz handler).
- **New infra**: `docker-compose.dev.yml` at repo root for local PostgreSQL.
- **New docs**: `backend/README.md` covering local dev and configuration.
- **New env vars** (backend-only, not exposed to the browser): `DATABASE_URL`, `HTTP_ADDR`, `LOG_LEVEL`, `ENV`.
- **No changes** to: `src/`, `package.json`, root `.env.example`, existing Supabase/Clerk integration. Those are explicitly the domain of subsequent phases.
- **No data migration** required; database is empty at this phase, and the project has no real user data to preserve.
- **Dependencies introduced** (Go side only): `github.com/gin-gonic/gin`, `gorm.io/gorm`, `gorm.io/driver/postgres`, `github.com/spf13/viper`.
