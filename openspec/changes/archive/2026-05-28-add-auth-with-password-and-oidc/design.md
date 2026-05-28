## Context

Phase 1 (`add-go-backend-skeleton`, archived) shipped a runnable Go service with `/healthz`, GORM, viper config, and an empty model registry. The frontend continues to use **Clerk** for identity and **Supabase** (PostgREST + RLS) for data. This change (Phase 2) removes both: identity moves into the Go backend, and every read/write of `projects`, `canvas_elements`, `user_credits` flows through new Go HTTP endpoints against our own PostgreSQL.

Decisions already locked by upstream Q&A (binding for this design):

- **Range**: backend + frontend + full Clerk/Supabase removal (no partial cohabitation).
- **Session model**: JWT access token (self-contained, short-lived) + DB-tracked opaque refresh token (revocable, long-lived).
- **OIDC providers**: Google only in this phase; provider table designed to add more later.
- **Extras included**: email verification, password reset, same-email account merge, CSRF, rate limiting on auth endpoints.
- **Password hashing**: argon2id via `alexedwards/argon2id`.
- **OIDC library**: `coreos/go-oidc/v3` + `golang.org/x/oauth2`.

## Goals / Non-Goals

**Goals**

- End-to-end self-hosted auth: register, verify email, log in (password or Google), refresh, log out, reset password — all served by the Go backend.
- A single ownership story for every business read/write: browser → Go (cookie + CSRF) → PostgreSQL. No more Clerk JWTs, no more Supabase direct.
- A frontend that works against the Go backend in dev with **zero CORS pain** (Next.js dev proxy rewrites `/api/*` → `http://localhost:8080`).
- Hard removal of `@clerk/*` and `@supabase/*` from `package.json`, plus every related env var and doc.
- Tests-as-scenarios coverage for every auth requirement (see `specs/backend-auth/spec.md`).

**Non-Goals**

- Additional OIDC providers, MFA, magic links, admin UI — explicitly future work.
- Production hardening: Redis-backed rate limit, secret-manager integration, mTLS to PG, log shipping. Local-dev parity only.
- Migration tooling beyond GORM AutoMigrate. We accept its destructive-op limits and document workarounds.
- Real production data migration. Local-dev seeding only; the project has no production users yet.

## Architecture overview

```
Browser
  ├── React pages + useAuth() reading /api/auth/me ──────────────┐
  ├── api.ts wrappers (same-origin, cookie auto-sent, CSRF hdr)   │
  └── Next.js middleware route guard                               │
                                                                   ▼
                              Next.js dev rewrite  /api/*  ──►  Go backend :8080
                                                                   │
                                                                   ▼
                                            Gin engine + middlewares
                                            ├── CORS (only when called cross-origin)
                                            ├── RequestID + slog access log
                                            ├── Rate limit (per route + per identifier)
                                            ├── CSRF (cookie-auth requests only)
                                            ├── RequireAuth (where applicable)
                                            └── Handlers
                                                ├── /api/auth/*   (auth package)
                                                └── /api/projects, /api/credits, ...
                                                              │
                                                              ▼
                                                        GORM ──► PostgreSQL
```

## Decisions

### D1. Session model: JWT access (RS256 + JWKS) + DB-tracked refresh

- **Access token**: RS256 JWT, 15-minute TTL, signed with the backend's RSA private key. Claims: `sub` (user UUID string), `email`, `email_verified`, `iat`, `exp`, `jti`, `iss="openlovart"`, `aud="openlovart-web"`, plus a `kid` header that identifies the signing key. Carried two ways:
  - `__Host-access` cookie: `HttpOnly`, `Secure` (configurable off for dev), `SameSite=Lax`, `Path=/`. This is what the browser uses.
  - `Authorization: Bearer <jwt>` header: still accepted, useful for CLI/tests and for any internal service-to-service call.
- **Signing keys (RSA 2048+)**: stored on disk as PEM files (`AUTH_JWT_PRIVATE_KEY_PATH`, `AUTH_JWT_PUBLIC_KEY_PATH`). Each key carries a deterministic `kid` (e.g., `sha256(public-key-DER)[:16]`). On startup the backend loads the active private key for signing and exposes **all known public keys** (current + any "previous" still inside its access-token TTL grace window) on `GET /api/auth/.well-known/jwks.json`. A single key is fine for this phase; the JWKS shape is forward-compatible with rotation.
- **Dev key bootstrap**: if both PEM paths are unset *and* `ENV=dev`, the backend generates an ephemeral 2048-bit RSA key pair into a stable on-disk location (`backend/.dev-keys/jwt.{pem,pub.pem}`, gitignored) on first run so `go run ./cmd/server` works zero-config. In any other environment, missing key paths is a startup error.
- **Refresh token**: 32-byte random, base64url-encoded → 43 chars. Stored in DB as `sha256(token)` only (never plain). Row contains `user_id`, `token_hash`, `created_at`, `expires_at` (30 days), `revoked_at`, `replaced_by_id` (for rotation chain), `user_agent`, `ip_at_issue`. Carried in `__Host-refresh` cookie scoped to `Path=/api/auth/refresh` (so it's only ever sent to that endpoint — minimizes exposure).
- **Refresh flow**: `POST /api/auth/refresh` reads the refresh cookie, looks up the row, verifies not revoked / not expired, **rotates** (creates a new refresh row, marks the old one revoked with `replaced_by_id` pointing to the new row, issues both new cookies, and refreshes the `csrf` cookie — see D4). Re-use of a revoked refresh token (refresh-token reuse detection) revokes the entire chain and forces re-login.
- **Logout**: `POST /api/auth/logout` revokes the current refresh row and clears all three cookies (access, refresh, csrf).
- **`/api/auth/me`**: reads access cookie (or bearer), returns `{ id, email, email_verified, created_at, providers: ["password","google"] }`. 401 if missing/expired/invalid. **Does not** rotate or re-issue the `csrf` cookie (see D4).

**Rationale**: RS256 + JWKS lets the Next.js process (and any future verifier) check signatures using the public key only — no shared secret coupling between processes. Self-contained access avoids a DB hit per request; DB-tracked refresh gives us revocation + multi-device logout + reuse detection. Splitting cookies by path narrows refresh exposure.

**Alternatives rejected**

- HS256 with shared secret: simpler but couples the Next.js and Go processes via a symmetric key; any key leak from either side compromises both.
- Pure JWT (no refresh table): no real revocation; logout is only client-side.
- Opaque sessions only (DB hit per request): simpler but adds latency to every authenticated request.

### D2. Password hashing: argon2id via `alexedwards/argon2id`

- Defaults: `Memory: 64*1024 KiB`, `Iterations: 1`, `Parallelism: 2`, `SaltLength: 16`, `KeyLength: 32`. (Tunable via constants in the package; documented in `backend/README.md`.)
- Hash + parameters stored as the encoded string the library produces (`$argon2id$v=19$m=65536,t=1,p=2$…$…`) in `identities.secret`.
- On login, `argon2id.ComparePasswordAndHash` (constant-time) + `argon2id.CheckHash` to detect outdated parameters and silently re-hash on successful login.

**Rationale**: argon2id is the OWASP-recommended default; `alexedwards/argon2id` wraps `golang.org/x/crypto/argon2` with safe defaults and a parameter-string format that makes upgrades trivial.

### D3. Google OIDC via `coreos/go-oidc/v3` + `golang.org/x/oauth2`

- Discovery: `oidc.NewProvider(ctx, "https://accounts.google.com")` at startup; verifier built from the provider's JWKS (auto-rotated by the library).
- Authorization Code + PKCE: backend generates `state` (random) and a `code_verifier`, then stores `{state, code_verifier, nonce, next}` HMAC-signed (using a dedicated `AUTH_OIDC_STATE_SECRET`, separate from the JWT signing keys) in a short-lived `__Host-oidc-state` cookie.
- `/api/auth/oidc/google/start` → 302 to Google with `state`, `code_challenge`, `nonce`.
- `/api/auth/oidc/google/callback` → validates `state` against cookie, exchanges code → ID-token, verifies signature + `aud` + `iss` + `nonce` + `email_verified == true`. Extracts `email`, `email_verified`, `sub` (Google's user id).
- **Same-email merge**: if a `users` row exists for that email **and** that row is `email_verified_at IS NOT NULL`, attach an `identities` row `(provider="oidc:google", subject=<google_sub>)` and log the user in. If the existing user is **not** verified (registered with password but never verified) we still merge but mark the email as verified now (Google has verified it for us, and we required `email_verified == true` from Google). If no user exists, create one with `email_verified_at = now()` and an `identities` row.
- After successful auth, issue the same `__Host-access` + `__Host-refresh` + `csrf` cookies as password login, then 302 to a frontend success URL (`/oidc/callback?status=ok&next=…`).

**Rationale**: PKCE is required by Google for confidential clients only when using public clients, but it doesn't hurt for confidential clients and makes the code path symmetric. Same-email merge is the user-friendly default and matches what most consumer products do; it is safe because Google's `email_verified=true` is a strong assertion. Using a separate HMAC secret for the OIDC state cookie keeps it independent from the JWT signing-key rotation lifecycle.

### D4. CSRF: double-submit cookie (issued only on credential events)

- A `csrf` cookie (`HttpOnly=false`, `SameSite=Lax`, `Secure`, `Path=/`) is set **only** by endpoints that issue or re-issue a session: `POST /api/auth/login`, `POST /api/auth/register`, `POST /api/auth/refresh`, and the OIDC callback. Value = 32-byte random base64url. `Max-Age` matches `AUTH_ACCESS_TTL` (15 min) so the CSRF cookie lifecycle tracks the access token.
- `GET /api/auth/me` does **not** rotate the CSRF cookie. Endpoints that don't change session state never touch it.
- For every `POST/PUT/PATCH/DELETE` to `/api/*` **authenticated by cookie** (not by `Authorization: Bearer`), the middleware requires `X-CSRF-Token` header to equal the `csrf` cookie value. Constant-time compare.
- The frontend's `fetchWithCsrf` reads the `csrf` cookie (via `document.cookie`) and injects the header automatically. If the CSRF cookie is missing on a state-changing call (typical after long idle periods where the access cookie has also expired), `fetchWithCsrf` first calls `POST /api/auth/refresh` (which re-issues all three cookies if the refresh cookie is still valid) and retries once; if refresh also fails, the helper surfaces a 401 to the caller, which routes the user to `/sign-in`.
- `Bearer`-authenticated calls are exempt (cookies are not in play). OIDC callback is exempt (top-level navigation, not an XHR; it's the endpoint that issues the cookies in the first place).

**Rationale**: Double-submit is simpler than synchronizer-token, doesn't require server-side state, and is enough given `SameSite=Lax` on the access cookie (which already blocks most cross-site POSTs). Tying CSRF cookie issuance to credential events (instead of every authenticated GET) keeps `/api/auth/me` cheap and stateless; the recovery path through `/api/auth/refresh` covers the long-idle case without per-request rotation overhead.

### D5. Rate limiting

- Library: `golang.org/x/time/rate` (token-bucket, in-memory). Pluggable via `Limiter` interface so prod can swap in a Redis-backed implementation.
- Keys per endpoint:
  - `/api/auth/login`: per IP **and** per email (whichever fires first; per-email defends against credential stuffing across IPs). Defaults: 10/min per IP, 5/min per email.
  - `/api/auth/register`: 20/hour per IP.
  - `/api/auth/forgot-password`, `/api/auth/verify-email/resend`: 5/hour per IP **and** per email.
  - `/api/auth/oidc/google/callback`: 30/min per IP.
- Exceeding the limit → HTTP 429 with `Retry-After` header. No body details (don't leak whether the email exists).
- Sweeper goroutine evicts buckets idle for >1h to bound memory.

**Rationale**: Per-IP alone is bypassable with a botnet; combining per-IP and per-identifier covers the realistic attacker models we care about at this stage.

### D6. Email verification

- On register: create `users` row with `email_verified_at = NULL`, create a `verification_tokens` row (`token_hash`, `user_id`, `expires_at = now()+24h`), send email containing `EMAIL_VERIFY_BASE_URL/?token=<plain>`.
- `GET /api/auth/verify-email?token=…` looks up by hash, checks expiry/not-used, sets `users.email_verified_at = now()`, marks token used, redirects to frontend.
- **Login policy**: unverified users **can** log in but every authenticated response includes `email_verified=false` so the frontend can show a verification banner. Endpoints that require verification (none in this phase) would check the JWT claim. Frontend gating happens at the page level.
- Resend endpoint: rate-limited as above; never reveals whether email exists.

**Rationale**: Soft gate (login allowed) is friendlier than hard gate and lets the user reach their dashboard even if the email is delayed. We don't gate business actions in this phase, but the model supports it.

### D7. Password reset

- `POST /api/auth/forgot-password { email }`: always returns 200 (don't reveal existence). If a verified user exists, create `password_reset_tokens` row (hash, expiry 1h), send `PASSWORD_RESET_BASE_URL/?token=<plain>`.
- `POST /api/auth/reset-password { token, new_password }`: validates token, sets the user's password identity (creating it if the user originally only had OIDC), marks token used, **revokes all refresh tokens for the user** (forces other devices to re-login).

**Rationale**: Revoking refresh tokens on password change is standard hygiene; creating a password identity on demand lets OIDC-only users add a password without a separate flow.

### D8. Account model and merging

```
users
  id (uuid pk)
  email (citext unique, not null)
  email_verified_at (timestamptz null)
  legacy_clerk_id (text null, unique nullable)  -- one-shot bridge for any legacy seed data
  created_at, updated_at

identities
  id (uuid pk)
  user_id (uuid fk users.id, on delete cascade)
  provider (text)       -- "password" | "oidc:google" | (future: "oidc:github")
  subject (text)        -- for "password": equals user_id; for OIDC: provider's user sub
  secret (text null)    -- argon2id encoded string for "password"; null for OIDC
  created_at, updated_at
  UNIQUE (provider, subject)
  UNIQUE (user_id, provider)   -- one identity per provider per user
```

- On password register with an existing email: if existing user has `email_verified_at IS NULL` and **no** password identity, replace any previous unverified attempts with a new password identity + re-send verification. If verified or has password identity → return generic "email already in use" (don't enumerate).
- On Google login with an existing verified email: attach OIDC identity (merge). On Google login with unverified email row: also merge and verify (Google asserted `email_verified=true`).
- On Google login with no existing user: create user + OIDC identity, `email_verified_at = now()`.

**Rationale**: Single `users` row per email is the least confusing UX and avoids "I have two accounts now" complaints. The verification gate prevents account hijack via "register with someone else's email, then they sign in with Google" because the OIDC merge requires Google's own `email_verified` assertion.

### D9. Cookie configuration

- All cookies use `__Host-` prefix when `AUTH_COOKIE_SECURE=true` (the only way the prefix is valid; the prefix forbids `Domain` and requires `Secure`+`Path=/` — except `__Host-refresh` which sets `Path=/api/auth/refresh`).
- In dev (`ENV=dev` and `AUTH_COOKIE_SECURE=false`), the prefix is dropped automatically and cookies are issued as plain `access`/`refresh`/`csrf` over HTTP. Documented in README.
- `SameSite=Lax` (not `Strict`) so OIDC callback redirect from Google preserves cookies. `Strict` would break the OIDC flow.

### D10. CORS

- Off by default. Enabled when `FRONTEND_ORIGIN` is set. Configuration:
  - `Access-Control-Allow-Origin: <FRONTEND_ORIGIN>` (single value, exact match — no wildcard with credentials)
  - `Access-Control-Allow-Credentials: true`
  - `Access-Control-Allow-Methods: GET, POST, PUT, PATCH, DELETE, OPTIONS`
  - `Access-Control-Allow-Headers: Content-Type, Authorization, X-CSRF-Token`
  - `Access-Control-Expose-Headers: X-Request-Id`
- In local dev we **don't** rely on CORS at all: a `next.config.ts` rewrite proxies `/api/*` → `http://localhost:8080`, so the browser sees same-origin. CORS is the production path.

### D11. Frontend session shape

- Server components: `getServerSession()` helper reads `__Host-access` from the request cookies and verifies the JWT signature **using the backend's JWKS** (`GET /api/auth/.well-known/jwks.json`). Verification uses `jose` (`createRemoteJWKSet` with `jwtVerify`); the JWKS is cached in-process for `AUTH_JWKS_CACHE_TTL` (default 5 min) and refetched on `kid` miss. The Next.js process never possesses the signing private key. Returns `{ user } | null`. Used by route guards and the `/lovart/user` initial render.
- Client components: `useAuth()` hook fetches `/api/auth/me` on mount, caches in a React Context, and exposes `{ user, loading, refresh, signOut }`. Sign-in/sign-up forms POST to `/api/auth/login` and `/api/auth/register`, on success they `router.replace(next ?? '/lovart')`.

**Rationale**: JWKS-based verification means the Next.js process holds nothing more sensitive than the backend's public key (which is, by definition, public). Compromise of the Next.js process does not compromise the ability to mint tokens — only the Go backend, which holds the private key, can sign. This is the textbook decoupling for two independently deployed services.

**Cost trade-off**: First request after process start performs one JWKS fetch (~1 ms over loopback in dev, low-tens of ms over WAN in prod), then in-memory for the cache TTL. Acceptable.

### D12. Next.js dev proxy

`next.config.ts` gains:

```ts
async rewrites() {
  return [
    { source: '/api/auth/:path*',     destination: 'http://localhost:8080/api/auth/:path*' },
    { source: '/api/projects/:path*', destination: 'http://localhost:8080/api/projects/:path*' },
    { source: '/api/credits/:path*',  destination: 'http://localhost:8080/api/credits/:path*' },
  ];
}
```

The `/api/auth/:path*` rule covers both the credential endpoints **and** the JWKS path `/api/auth/.well-known/jwks.json`, so the Next.js process can fetch JWKS via `http://localhost:3000/api/auth/.well-known/jwks.json` over loopback (or directly via `http://localhost:8080/...` in server-only code — it has both options). Existing `/api/generate-*` and `/api/video-status` routes remain Next-served (they live in `src/app/api/`). The rewrite only applies to the auth/business prefixes.

**Rationale**: Targeted rewrites keep Next-owned API routes intact and avoid an "everything goes to Go" surprise.

### D13. Schema migration strategy

- AutoMigrate creates all new tables on first startup against the dev Postgres.
- `supabase-schema.sql` is removed (preserved in git history). Its `auth.jwt()`-based RLS policies are obsolete because the only DB client is the Go backend, which enforces ownership in service code.
- **No data-migration script ships with this change** — the project has no production data to carry over. If a developer needs to import legacy rows ad-hoc, they can do so with manual SQL; we don't preserve a reference script in-tree because it would otherwise sit unused indefinitely.
- `add-user-credits.sql` at repo root is deleted; the same intent (1000 starter credits) is encoded as a service-layer rule (`OnUserCreated` hook in the auth service creates a `user_credits` row with `credits=1000`).

### D14. Email transport: SMTP + Mailpit in dev

- `internal/email/sender.go` defines `Sender interface { Send(ctx, to, subject, html, text string) error }`.
- Default impl: `internal/email/smtp.go` using `gopkg.in/gomail.v2` (simple, no breaking changes since 2018, supports plain auth + TLS).
- `docker-compose.dev.yml` adds:
  ```yaml
  mailpit:
    image: axllent/mailpit:latest
    ports: ["1025:1025", "8025:8025"]
  ```
  Default dev env: `SMTP_HOST=localhost`, `SMTP_PORT=1025`, no auth. Developer opens `http://localhost:8025` to view captured mail.

**Rationale**: Mailpit is zero-config and captures everything; using a real provider in dev would create noise and require credentials in `.env.example`.

### D15. Errors and audit logging

- All auth handlers return a uniform JSON error shape: `{ "error": "code", "message": "human" }` with no detail leak (codes are intentionally coarse: `invalid_credentials`, `rate_limited`, `invalid_token`, `email_not_verified`, `internal_error`). The structured `slog` log captures the real detail server-side with `request_id`, `user_id` (when known), `ip`, `ua`.
- Auth events logged at `info`: `auth.register`, `auth.login.success`, `auth.login.failure`, `auth.refresh`, `auth.logout`, `auth.oidc.success`, `auth.password_reset.requested`, `auth.password_reset.completed`. These are the audit trail; no separate audit table in this phase.

### D16. Testing strategy (informational; tests are part of tasks)

- Per OpenSpec convention, scenarios in `specs/*` are the source of truth. Implementation tasks translate each scenario into a Go integration test (using `httptest.Server` + `gorm` against a transient Postgres via `testcontainers-go` or a developer-provided test DB URL).
- Frontend changes get smoke-checked via `npm run build` + manual flow in dev. No new E2E framework introduced in this phase.

## Risks / Trade-offs

- **JWKS cache staleness** between Next.js and Go means a key rotation can take up to `AUTH_JWKS_CACHE_TTL` (default 5 min) to fully propagate. We tolerate this because rotation is rare in this phase (one key, no scheduled rotation); any future rotation flow will publish the new key alongside the old in JWKS for at least one cache TTL before retiring the old key.
- **In-memory rate limit** doesn't survive process restarts and doesn't span replicas. Accepted; the `Limiter` interface lets us swap implementations later.
- **AutoMigrate** still can't do destructive changes. The user/identities split-out from a future "merge two users" flow may need a manual script. Documented in `backend/README.md`.
- **Email verification soft gate** means an attacker who registers `victim@example.com` could lock the real owner out for 24h (until token expires). Mitigation: forgot-password flow lets the owner reclaim by verifying via the original-email link they generate. We accept this trade-off because the alternative (hard gate) hurts onboarding more than it helps.
- **Same-email OIDC merge** is intentional and matches industry norms but has one residual risk: if a user *changes* their Google account email to one that already exists in our system (rare), they could in principle log in as that user. Google does not allow arbitrary email changes on consumer accounts, and we require `email_verified=true`, so the residual risk is acceptable.
- **CSRF cookie issuance only on credential events** (D4) means a user whose access cookie has expired but whose refresh cookie is still valid will see one extra round-trip (`/api/auth/refresh`) before their next state-changing request succeeds. The frontend's `fetchWithCsrf` handles this transparently; the user-visible cost is ~50 ms in dev.
- **Cookie size**: `__Host-access` JWT (RS256) is ~700 bytes, `__Host-refresh` is 43 bytes, `csrf` is 44 bytes. Total well under the 4 KB per-cookie limit.
- **Bearer auth bypassing CSRF** (D4) is by design: bearer is for non-browser callers, where CSRF doesn't apply. Frontend never uses bearer.
- **Deleting `@supabase/supabase-js`** removes the only Supabase usage. If we later want to use Supabase Storage for image hosting, we can add the package back at that point; nothing else in this change forecloses that.
- **`/api/projects` cursor pagination** introduces stateful cursor encoding (`base64url(updated_at|id)`); cursor parsing failures must return 400 to avoid degrading silently to "list all". Covered in spec scenarios.

## Migration Plan

This change is **breaking** at the session boundary (existing Clerk sessions are invalidated) but the project has no production users, so we treat it as a hard cutover:

1. **Backend lands first** (Phase 2a in tasks): all auth + business endpoints implemented and unit-tested against the dev Postgres + Mailpit. Verified standalone via `curl`/Postman.
2. **Frontend lands second** (Phase 2b): Clerk + Supabase removed in the same commit set that adds the new pages and `api.ts`. We do **not** ship a half-state where both stacks are present, because that requires keeping `auth.jwt()`-based RLS alive, which contradicts the goal.
3. **Schema cutover**: drop the Supabase tables locally (or just abandon the Supabase project); the Go backend creates the new schema via AutoMigrate on first run.
4. **Smoke flow** (run end-to-end before declaring complete):
   - register with password → verification email shows up in Mailpit → click link → user is verified → log in succeeds → `/lovart` loads → create a project → reload → project still there.
   - log out → log in with Google → see same email's projects (merge worked).
   - forgot password → email arrives → reset → log in with new password → old refresh token rejected on `/api/auth/refresh`.
   - hit `/api/auth/login` 11 times in <1 min → 11th returns 429.

**Rollback**: revert the change set. PG dev data is disposable. No prod state.

## Open Questions

All open questions resolved during proposal Q&A. Final decisions captured here for traceability:

- **O1. Where should the JWT verification material live for the Next.js process?** Resolved: **JWKS-based verification (RS256)**. The Go backend exposes `GET /api/auth/.well-known/jwks.json`; the Next.js process fetches and caches the JWKS via `jose.createRemoteJWKSet`. No symmetric secret crosses the process boundary. See D1 and D11.
- **O2. Should `/api/auth/me` set fresh CSRF cookie on every call?** Resolved: **No**. The CSRF cookie is issued only by credential-issuing endpoints (`login`, `register`, `refresh`, OIDC callback). Long-idle clients recover via a transparent `fetchWithCsrf` → `refresh` → retry flow. See D4.
- **O3. Pagination on `/api/projects`?** Resolved: **Yes, cursor-based, in this phase**. `GET /api/projects?limit=20&cursor=…` returns `{items, nextCursor}`; the cursor is `base64url(updated_at|id)`; default limit 20, max 100. Frontend uses paginated fetch with a "Load more" button on the projects list. See `backend-business-api/spec.md`.
- **O4. Soft delete vs hard delete for projects?** Resolved: **Hard delete**. `DELETE FROM projects WHERE id=...` cascades to `canvas_elements`. Matches current Supabase semantics; soft delete is deferred to a future change if/when a recycle-bin UI is introduced.
- **O5. Where do we put one-shot Postgres helpers?** Resolved: **No script in-tree**. The project has no production data; ad-hoc imports, if ever needed, are handled with manual SQL (see D13). No `migrations/` directory under this change.
