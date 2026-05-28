# backend-runtime

## Purpose

Defines the runtime behavior of the self-hosted Go backend service: process entry point, configuration loading, structured logging, HTTP server, health endpoint, and graceful shutdown.

## Requirements

### Requirement: Backend service binary

The system SHALL provide a single Go binary entry point at `backend/cmd/server` that, when invoked, loads configuration, initializes logging and the database connection, runs schema migration, starts the HTTP server, and blocks until the process is signaled to shut down.

#### Scenario: Successful startup

- **WHEN** an operator runs `go run ./cmd/server` from `backend/` with a reachable PostgreSQL instance and valid configuration
- **THEN** the process logs a startup message including the listen address, opens a database connection pool, executes AutoMigrate (a no-op when no models are registered), begins serving HTTP requests on the configured address, and remains running until interrupted

#### Scenario: Database unreachable at startup

- **WHEN** the process starts but cannot connect to PostgreSQL using `DATABASE_URL` after a bounded retry/timeout window
- **THEN** the process logs an error describing the failure and exits with a non-zero status code without binding the HTTP listener

### Requirement: Configuration loading

The system SHALL load configuration via viper from environment variables and an optional `backend/.env` file. Environment variables MUST take precedence over `.env`. The system MUST apply documented defaults for any unset value.

#### Scenario: All values from environment

- **WHEN** the process starts with all configuration variables provided as environment variables
- **THEN** the loaded `Config` struct reflects exactly those environment values without consulting any `.env` file

#### Scenario: Mixed sources

- **WHEN** the process starts with some values in `backend/.env` and others overridden by environment variables
- **THEN** the loaded `Config` struct contains the environment value for any key set in both, and the `.env` value for any key set only there

#### Scenario: Defaults applied

- **WHEN** the process starts with no environment variables and no `.env` file
- **THEN** the loaded `Config` struct contains `ENV=dev`, `HTTP_ADDR=:8080`, `LOG_LEVEL=info`, and `DATABASE_URL=postgres://openlovart:openlovart@localhost:5432/openlovart?sslmode=disable`

### Requirement: Structured logging

The system SHALL emit logs through `log/slog`. The handler format MUST be selected by the `ENV` configuration value, and the minimum log level MUST be controlled by `LOG_LEVEL`.

#### Scenario: Development log format

- **WHEN** `ENV=dev`
- **THEN** logs are emitted using the slog text handler in a human-readable form

#### Scenario: Production log format

- **WHEN** `ENV=prod`
- **THEN** logs are emitted using the slog JSON handler with one JSON object per log record

#### Scenario: Log level filtering

- **WHEN** `LOG_LEVEL=warn`
- **THEN** records logged at debug or info level are suppressed, and records at warn or error level are written

### Requirement: HTTP server using Gin

The system SHALL serve HTTP requests using the Gin framework, wrapped in `net/http.Server` so that timeouts and graceful shutdown are configurable independently of Gin. The router MUST mount the route groups introduced by this change (`/api/auth/*` including the public JWKS endpoint at `/api/auth/.well-known/jwks.json`, `/api/projects/*`, `/api/credits`, `/api/projects/:id/canvas-elements`), apply the request-id, structured-access-log, and CORS middlewares globally, and apply `RequireAuth` + `CSRF` middlewares to the business route group. The JWKS endpoint MUST NOT be guarded by `RequireAuth` or `CSRF`.

#### Scenario: Server binds to configured address

- **WHEN** the process starts with `HTTP_ADDR=:9090`
- **THEN** the HTTP server accepts connections on TCP port 9090

#### Scenario: Unrecognized route

- **WHEN** a client requests a path that has no registered handler
- **THEN** the server responds with HTTP 404 with body `{"error":"not_found"}`

#### Scenario: Every response carries a request id

- **WHEN** a client makes any request to the server
- **THEN** the response includes an `X-Request-Id` header (echoed from the request when the client provided one, otherwise a server-generated UUID) and the corresponding slog access-log entry includes the same id

#### Scenario: JWKS endpoint is publicly mounted

- **WHEN** an unauthenticated client GETs `/api/auth/.well-known/jwks.json`
- **THEN** the request reaches the JWKS handler without `RequireAuth` or `CSRF` rejecting it, and the response is HTTP 200 with the JWK Set body

### Requirement: Health endpoint

The system SHALL expose `GET /healthz` **and** `GET /api/health` returning identical responses. Both MUST report process liveness and database reachability and MUST complete within 1 second by enforcing a database ping timeout. The duplicate `/api/health` path exists so that the frontend's same-origin probe can reach the backend through the Next.js `/api/*` rewrite without special-casing the path.

#### Scenario: Healthy via /healthz

- **WHEN** the process is running and the database ping succeeds within 1 second
- **THEN** `GET /healthz` returns HTTP 200 with JSON body `{"status":"ok","db":"ok"}`

#### Scenario: Healthy via /api/health

- **WHEN** the process is running and the database ping succeeds within 1 second
- **THEN** `GET /api/health` returns HTTP 200 with JSON body `{"status":"ok","db":"ok"}` (byte-identical to `/healthz`)

#### Scenario: Database degraded

- **WHEN** the process is running but the database ping fails or times out
- **THEN** both `/healthz` and `/api/health` return HTTP 503 with JSON body `{"status":"degraded","db":"down"}`

### Requirement: Graceful shutdown

The system SHALL handle `SIGINT` and `SIGTERM` by stopping acceptance of new connections, allowing in-flight HTTP requests to complete within a 10-second deadline, and closing the database connection pool before exiting.

#### Scenario: Clean shutdown

- **WHEN** the process receives SIGINT while idle
- **THEN** the HTTP server stops, the database pool is closed, and the process exits with status 0 within 10 seconds

#### Scenario: Shutdown deadline reached

- **WHEN** the process receives SIGTERM while a long-running request exceeds the 10-second deadline
- **THEN** the server forces termination of remaining connections, closes the database pool, and exits with a non-zero status code

### Requirement: CORS middleware

The system SHALL apply CORS handling when `FRONTEND_ORIGIN` is set in configuration. Allowed origin MUST be exactly that single value (no wildcards with credentials). Allowed credentials MUST be `true`. Allowed methods MUST include `GET, POST, PUT, PATCH, DELETE, OPTIONS`. Allowed headers MUST include `Content-Type`, `Authorization`, `X-CSRF-Token`. Exposed headers MUST include `X-Request-Id`. `OPTIONS` preflight requests MUST short-circuit with HTTP 204 and not invoke downstream handlers.

#### Scenario: Preflight is answered without invoking handler

- **WHEN** a client sends `OPTIONS /api/projects` with `Origin: <FRONTEND_ORIGIN>` and `Access-Control-Request-Method: POST`
- **THEN** the system returns HTTP 204 with `Access-Control-Allow-Origin: <FRONTEND_ORIGIN>`, `Access-Control-Allow-Credentials: true`, the configured method/header lists, and **does not** call the `/api/projects` handler

#### Scenario: Cross-origin actual request is allowed

- **WHEN** a client sends `POST /api/projects` with `Origin: <FRONTEND_ORIGIN>` and a valid session cookie + CSRF header
- **THEN** the response includes `Access-Control-Allow-Origin: <FRONTEND_ORIGIN>` and `Access-Control-Allow-Credentials: true`

#### Scenario: CORS disabled when origin unset

- **WHEN** the server is started without `FRONTEND_ORIGIN`
- **THEN** no `Access-Control-*` headers are added, and `OPTIONS` requests fall through to the default 404 behavior

### Requirement: Rate-limit middleware integration

The system SHALL provide a rate-limit middleware that the router uses on individual auth endpoints per the limits specified in the `backend-auth` capability. The middleware MUST be injected via a `Limiter` interface so that an alternative implementation (e.g., Redis-backed) can be substituted without changes to the router or handlers.

#### Scenario: Limiter swap is transparent

- **WHEN** the process is constructed with an alternative `Limiter` implementation
- **THEN** the router, middleware, and handlers compile and function unchanged; only the construction site in `cmd/server/main.go` differs

#### Scenario: Limiter sweep frees memory

- **WHEN** the in-memory limiter holds buckets that have been idle for longer than the configured sweep interval (default 1 hour)
- **THEN** the background sweeper removes those buckets so that memory does not grow unbounded with unique IPs
