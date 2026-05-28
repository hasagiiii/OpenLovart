# backend-persistence

## Purpose

Defines persistence-layer behavior for the Go backend: PostgreSQL connectivity via GORM, the schema-migration registry, GORM-to-slog log integration, and connection-pool teardown.

## Requirements

### Requirement: PostgreSQL connection via GORM

The system SHALL connect to PostgreSQL using GORM v2 with the `gorm.io/driver/postgres` driver. The DSN MUST be sourced from the `DATABASE_URL` configuration value. The connection pool MUST apply explicit limits for max open connections, max idle connections, and connection lifetime.

#### Scenario: Successful connection at startup

- **WHEN** the process starts with a valid `DATABASE_URL` pointing to a reachable PostgreSQL instance
- **THEN** a GORM `*gorm.DB` is opened, an underlying `*sql.DB` is configured with `MaxOpenConns=20`, `MaxIdleConns=5`, and `ConnMaxLifetime=1h`, and a successful `Ping` against the database is performed before startup proceeds

#### Scenario: Invalid DSN

- **WHEN** the process starts with a malformed `DATABASE_URL`
- **THEN** the process logs the parsing error and exits with a non-zero status code

### Requirement: Model registry for AutoMigrate

The system SHALL provide a single registration mechanism through which model packages declare GORM model types to be migrated. AutoMigrate MUST iterate over the registered set. The registry MUST be populated at process startup with the auth and business models defined in this change, so that AutoMigrate produces a complete schema on a fresh database.

#### Scenario: Auth and business models migrated on first run

- **WHEN** the process starts against an empty PostgreSQL database with the auth and business models registered
- **THEN** AutoMigrate creates tables `users`, `identities`, `refresh_tokens`, `verification_tokens`, `password_reset_tokens`, `projects`, `canvas_elements`, `user_credits` with their declared columns, indexes, and foreign keys; the process proceeds to start the HTTP server only after AutoMigrate returns nil

#### Scenario: Subsequent startup is idempotent

- **WHEN** the process starts a second time against the same database
- **THEN** AutoMigrate detects existing tables/columns/indexes and returns nil without recreating or altering them in lossy ways

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

### Requirement: Required PostgreSQL extensions

The system SHALL ensure required PostgreSQL extensions are present before AutoMigrate runs. At minimum it MUST execute `CREATE EXTENSION IF NOT EXISTS "citext"` (used for case-insensitive uniqueness on `users.email`) and `CREATE EXTENSION IF NOT EXISTS "uuid-ossp"` (for server-side UUID defaults where applicable). Failure to create an extension MUST abort startup with a non-zero status code.

#### Scenario: Fresh database gets extensions

- **WHEN** the process starts against a database where `citext` is not yet installed
- **THEN** the system runs `CREATE EXTENSION IF NOT EXISTS "citext"` successfully before AutoMigrate, and the `users.email` column is created as `citext` with a unique index

#### Scenario: Extension already present

- **WHEN** the process starts against a database where `citext` is already installed
- **THEN** the `CREATE EXTENSION IF NOT EXISTS` is a no-op and startup proceeds normally

#### Scenario: Insufficient privileges to create extension

- **WHEN** the database user lacks `CREATE` privilege on the database and the extension is missing
- **THEN** the process logs the underlying PostgreSQL error and exits with a non-zero status code without starting the HTTP listener

### Requirement: Transaction boundaries for multi-row writes

The system SHALL execute every multi-row write in a single GORM transaction so that partial updates never persist. Specifically:
- User creation (whether via password registration or OIDC first sign-in) MUST insert the `users`, `identities`, and `user_credits` rows in one transaction; failure of any insert MUST roll back the others.
- Canvas-elements replace-all MUST execute the delete + bulk-insert in one transaction.
- Refresh-token rotation MUST mark the old row revoked and insert the new row in one transaction.

#### Scenario: Atomic user creation

- **WHEN** the system attempts to register a new user and the `user_credits` insert fails (for example, due to a transient database error)
- **THEN** the entire transaction rolls back, leaving no `users` row and no `identities` row, so the user can safely retry registration

#### Scenario: Atomic canvas-elements replace

- **WHEN** the system processes `PUT /api/projects/:id/canvas-elements` and an insert in the bulk write fails
- **THEN** the prior set of elements remains intact (the delete is rolled back together with the failed inserts) and the response is HTTP 500
