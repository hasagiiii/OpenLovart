## ADDED Requirements

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

The system SHALL serve HTTP requests using the Gin framework, wrapped in `net/http.Server` so that timeouts and graceful shutdown are configurable independently of Gin.

#### Scenario: Server binds to configured address

- **WHEN** the process starts with `HTTP_ADDR=:9090`
- **THEN** the HTTP server accepts connections on TCP port 9090

#### Scenario: Unrecognized route

- **WHEN** a client requests a path that has no registered handler
- **THEN** the server responds with HTTP 404

### Requirement: Health endpoint

The system SHALL expose `GET /healthz`. The endpoint MUST report both process liveness and database reachability, and MUST complete within 1 second by enforcing a database ping timeout.

#### Scenario: Healthy

- **WHEN** the process is running and the database ping succeeds within 1 second
- **THEN** the endpoint returns HTTP 200 with JSON body `{"status":"ok","db":"ok"}`

#### Scenario: Database degraded

- **WHEN** the process is running but the database ping fails or times out
- **THEN** the endpoint returns HTTP 503 with JSON body `{"status":"degraded","db":"down"}`

### Requirement: Graceful shutdown

The system SHALL handle `SIGINT` and `SIGTERM` by stopping acceptance of new connections, allowing in-flight HTTP requests to complete within a 10-second deadline, and closing the database connection pool before exiting.

#### Scenario: Clean shutdown

- **WHEN** the process receives SIGINT while idle
- **THEN** the HTTP server stops, the database pool is closed, and the process exits with status 0 within 10 seconds

#### Scenario: Shutdown deadline reached

- **WHEN** the process receives SIGTERM while a long-running request exceeds the 10-second deadline
- **THEN** the server forces termination of remaining connections, closes the database pool, and exits with a non-zero status code
