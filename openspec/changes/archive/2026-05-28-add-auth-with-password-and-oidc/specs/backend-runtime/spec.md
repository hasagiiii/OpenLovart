## MODIFIED Requirements

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

## ADDED Requirements

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
