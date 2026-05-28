## ADDED Requirements

### Requirement: PostgreSQL connection via GORM

The system SHALL connect to PostgreSQL using GORM v2 with the `gorm.io/driver/postgres` driver. The DSN MUST be sourced from the `DATABASE_URL` configuration value. The connection pool MUST apply explicit limits for max open connections, max idle connections, and connection lifetime.

#### Scenario: Successful connection at startup

- **WHEN** the process starts with a valid `DATABASE_URL` pointing to a reachable PostgreSQL instance
- **THEN** a GORM `*gorm.DB` is opened, an underlying `*sql.DB` is configured with `MaxOpenConns=20`, `MaxIdleConns=5`, and `ConnMaxLifetime=1h`, and a successful `Ping` against the database is performed before startup proceeds

#### Scenario: Invalid DSN

- **WHEN** the process starts with a malformed `DATABASE_URL`
- **THEN** the process logs the parsing error and exits with a non-zero status code

### Requirement: Model registry for AutoMigrate

The system SHALL provide a single registration mechanism through which model packages declare GORM model types to be migrated. AutoMigrate MUST iterate over the registered set; an empty registry MUST be a valid, no-op state.

#### Scenario: Empty registry

- **WHEN** AutoMigrate runs with no registered models
- **THEN** the function returns without error and without altering the database schema

#### Scenario: Registered models migrated

- **WHEN** one or more model types are registered before AutoMigrate runs
- **THEN** GORM creates or updates the corresponding tables, columns, indexes, and constraints to match the registered structs

#### Scenario: AutoMigrate failure

- **WHEN** AutoMigrate encounters a schema operation that PostgreSQL rejects
- **THEN** the function returns the underlying error, the process logs it, and startup aborts with a non-zero status code

### Requirement: GORM logger integration

The system SHALL route GORM's internal log output through the application's `slog` logger so that GORM log records share the same destination, format, and level filtering as the rest of the backend.

#### Scenario: GORM log appears in slog output

- **WHEN** GORM emits an info-level log entry while `LOG_LEVEL=info`
- **THEN** the entry is rendered through the active slog handler with the same format used for application logs

#### Scenario: GORM log filtered by level

- **WHEN** GORM emits an info-level log entry while `LOG_LEVEL=warn`
- **THEN** the entry is suppressed, matching the suppression behavior of native slog records at the same level

### Requirement: Connection pool teardown

The system SHALL close the underlying database connection pool during graceful shutdown so that no connections are leaked when the process exits.

#### Scenario: Shutdown closes pool

- **WHEN** the server begins graceful shutdown
- **THEN** the underlying `*sql.DB` is closed before the process exits, releasing all pooled connections to PostgreSQL
