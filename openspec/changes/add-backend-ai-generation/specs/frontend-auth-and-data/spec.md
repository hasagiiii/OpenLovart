## ADDED Requirements

### Requirement: Generation features call the backend

The frontend generation features SHALL call the Go backend's `/api/ai/*`
endpoints over same-origin fetch using the cookie session, attaching the
`X-CSRF-Token` header for state-changing calls (i.e. via the existing
`fetchWithCsrf` helper). The chat/design experience (`DesignChat.tsx`) MUST
target `POST /api/ai/chat/completions` and consume the OpenAI-shaped response
(reading `choices[0].message.content`, or for streaming the concatenation of
`choices[0].delta.content` from `chat.completion.chunk` SSE events). The image
experience (`ImageGeneratorPanel.tsx`, `ImageGeneratorDialog.tsx`) MUST submit
to `POST /api/ai/images`, poll `GET /api/ai/images/:id/status`, and fetch
`GET /api/ai/images/:id` for the final `images[].url`. The frontend MUST NOT
hold or read any model/image provider API key.

#### Scenario: Design chat hits the backend

- **WHEN** a user sends a message in `DesignChat`
- **THEN** the component calls `POST /api/ai/chat/completions` with `credentials:'include'` and the `X-CSRF-Token` header, and renders the assistant content from the response

#### Scenario: Image generation polls to completion

- **WHEN** a user submits an image prompt in the image generator
- **THEN** the component POSTs `/api/ai/images`, receives a `request_id`, polls `/api/ai/images/:id/status` until `COMPLETED`, then GETs `/api/ai/images/:id` and displays the returned image url(s)

#### Scenario: No provider key in frontend source

- **WHEN** a developer greps `src/` for chat or image provider API keys / base URLs
- **THEN** no such literal exists in frontend source

## MODIFIED Requirements

### Requirement: Server-side session helper

The frontend SHALL expose `getServerSession()` from `src/lib/auth/server.ts` that reads the access cookie from the incoming request, verifies the JWT against the backend's JWKS using `jose.jwtVerify` with a `createRemoteJWKSet` instance (target: `${BACKEND_INTERNAL_URL}/api/auth/.well-known/jwks.json`, cached in-process for `AUTH_JWKS_CACHE_TTL`, default 300 s), and returns `{ user: {id, email, emailVerified} } | null`. Server components and Next-served API routes MUST use this helper for any session check; they MUST NOT call back to the Go backend's `/api/auth/me` for this purpose. The helper MUST NOT possess any signing secret; it relies solely on the public keys served by the JWKS endpoint.

#### Scenario: Valid session in a server component

- **WHEN** a server component calls `getServerSession()` during a request with a valid access cookie
- **THEN** the helper returns the parsed user object after verifying the JWT against a cached JWKS, with at most one network call to the JWKS endpoint per cache TTL window

#### Scenario: Expired token returns null

- **WHEN** the access cookie is present but its `exp` claim is in the past
- **THEN** `getServerSession()` returns `null` (the page can then render an unauthenticated state or trigger a redirect)

#### Scenario: Unknown kid triggers JWKS refetch

- **WHEN** the access cookie's JWT carries a `kid` not present in the cached JWKS
- **THEN** `getServerSession()` re-fetches the JWKS once; if the `kid` is then resolved, verification proceeds; if not, the helper returns `null`

#### Scenario: Remaining Next API routes use the helper

- **WHEN** either of the remaining Next-served generation routes (`src/app/api/generate-video`, `src/app/api/video-status`) is invoked
- **THEN** the route handler calls `getServerSession()` and responds HTTP 401 if it returns null; otherwise it uses `session.user.id` for downstream attribution

### Requirement: Development proxy for backend routes

In development (`NODE_ENV === 'development'`), `next.config.ts` SHALL rewrite `/api/auth/:path*`, `/api/projects/:path*`, `/api/credits/:path*`, and `/api/ai/:path*` to the corresponding `http://localhost:8080/...` paths so that the browser treats backend responses as same-origin. The rewrite MUST NOT apply to `/api/generate-video` or `/api/video-status` so that those Next-served routes remain functional during the period before video is migrated.

#### Scenario: Auth call goes to Go backend

- **WHEN** the browser POSTs to `/api/auth/login` during `npm run dev`
- **THEN** the request reaches the Go backend on port 8080 (verifiable via the backend's access log) and the browser treats the cookies returned by the Go backend as same-origin

#### Scenario: AI call goes to Go backend

- **WHEN** the browser POSTs to `/api/ai/chat/completions` or `/api/ai/images` during `npm run dev`
- **THEN** the request is rewritten to `http://localhost:8080/api/ai/...` and handled by the Go backend

#### Scenario: Video routes stay on Next

- **WHEN** the browser POSTs to `/api/generate-video` or GETs `/api/video-status` during `npm run dev`
- **THEN** the request is handled by the corresponding route under `src/app/api/` inside the Next process (no rewrite to port 8080)
