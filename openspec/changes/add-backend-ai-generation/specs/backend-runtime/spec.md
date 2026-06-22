## MODIFIED Requirements

### Requirement: HTTP server using Gin

The system SHALL serve HTTP requests using the Gin framework, wrapped in `net/http.Server` so that timeouts and graceful shutdown are configurable independently of Gin. The router MUST mount the route groups introduced by this change (`/api/auth/*` including the public JWKS endpoint at `/api/auth/.well-known/jwks.json`, `/api/projects/*`, `/api/credits`, `/api/projects/:id/canvas-elements`, and the AI generation routes `/api/ai/chat/completions`, `/api/ai/images`, `/api/ai/images/:id`, `/api/ai/images/:id/status`), apply the request-id, structured-access-log, and CORS middlewares globally, and apply `RequireAuth` + `CSRF` middlewares to the business route group (which includes the `/api/ai/*` routes). The JWKS endpoint MUST NOT be guarded by `RequireAuth` or `CSRF`.

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

#### Scenario: AI routes require authentication

- **WHEN** an unauthenticated client requests any `/api/ai/*` route
- **THEN** the `RequireAuth` middleware rejects it with HTTP 401 before any handler logic runs

## ADDED Requirements

### Requirement: AI provider configuration

The system SHALL load AI generation settings via the same viper-based
configuration mechanism (environment variables with optional `backend/.env`,
environment taking precedence). The chat model MUST be selected by
`AI_CHAT_MODEL` and authenticate using `OPENAI_API_KEY` (and optional
`OPENAI_BASE_URL`) read from the process environment as required by the
`trpc-agent-go` OpenAI model. The image provider MUST be selected by
`AI_IMAGE_PROVIDER` / `AI_IMAGE_MODEL` with its credential supplied via
configuration. A web search provider for the chat agent's `web_search` tool MAY
be configured via `AI_SEARCH_PROVIDER` (defaulting to `brave`) and
`AI_SEARCH_API_KEY`; when no search credential is configured the `web_search`
tool MUST be disabled rather than failing startup. No AI provider credential may
be required by, or present in, the frontend.

#### Scenario: Chat model configured from environment

- **WHEN** the process starts with `AI_CHAT_MODEL` and `OPENAI_API_KEY` set
- **THEN** the constructed chat agent uses that model and key, and startup does not fail for a missing chat credential

#### Scenario: Default chat model applied

- **WHEN** the process starts with `OPENAI_API_KEY` set but `AI_CHAT_MODEL` unset
- **THEN** the loaded configuration applies the documented default chat model

#### Scenario: Image provider configured from environment

- **WHEN** the process starts with `AI_IMAGE_PROVIDER`, `AI_IMAGE_MODEL`, and the provider credential set
- **THEN** the image service is constructed against that provider and model using the configured credential

#### Scenario: Web search provider optional

- **WHEN** the process starts without `AI_SEARCH_PROVIDER` / `AI_SEARCH_API_KEY`
- **THEN** startup succeeds and the chat agent runs with the `web_search` tool disabled

#### Scenario: Web search defaults to Brave

- **WHEN** the process starts with `AI_SEARCH_API_KEY` set but `AI_SEARCH_PROVIDER` unset
- **THEN** the loaded configuration applies the default `brave` provider and the `web_search` tool is registered against Brave
