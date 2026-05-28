## 1. Backend dependencies & config

- [ ] 1.1 Add backend dependencies via `go get`: `github.com/alexedwards/argon2id`, `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, `github.com/golang-jwt/jwt/v5`, `golang.org/x/time/rate`, `gopkg.in/gomail.v2`, `github.com/google/uuid`, `gorm.io/datatypes`; run `go mod tidy`
- [ ] 1.2 Extend `internal/config/Config` with the new keys: `FrontendOrigin`, `AuthJWTPrivateKeyPath`, `AuthJWTPublicKeyPath`, `AuthOIDCStateSecret`, `AuthAccessTTL` (duration), `AuthRefreshTTL` (duration), `AuthCookieDomain`, `AuthCookieSecure` (bool), `OIDCGoogleClientID`, `OIDCGoogleClientSecret`, `OIDCGoogleRedirectURL`, `SMTPHost`, `SMTPPort` (int), `SMTPUsername`, `SMTPPassword`, `SMTPFrom`, `EmailVerifyBaseURL`, `PasswordResetBaseURL`, `RateLimitLoginPerMin` (int), `RateLimitRegisterPerHour` (int)
- [ ] 1.3 Update viper defaults and validation: `AUTH_ACCESS_TTL=15m`, `AUTH_REFRESH_TTL=720h`, `AUTH_COOKIE_SECURE=false` (dev), `SMTP_HOST=localhost`, `SMTP_PORT=1025`, `RATE_LIMIT_LOGIN_PER_MIN=10`, `RATE_LIMIT_REGISTER_PER_HOUR=20`; require `AUTH_OIDC_STATE_SECRET` to be at least 32 bytes; require `AUTH_JWT_PRIVATE_KEY_PATH`+`AUTH_JWT_PUBLIC_KEY_PATH` (both readable PEM files) when `ENV != dev`
- [ ] 1.4 Update `backend/.env.example` to document every new variable with a comment explaining its purpose and dev-vs-prod implications; explicitly note that `AUTH_JWT_*_KEY_PATH` may be omitted in dev (auto-generated) but is required in prod, and that `AUTH_OIDC_STATE_SECRET` is independent of the JWT keys

## 2. Database models (GORM)

- [ ] 2.1 Create `internal/models/user.go`: `User{ID uuid.UUID; Email string (citext, unique, not null); EmailVerifiedAt *time.Time; LegacyClerkID *string; CreatedAt, UpdatedAt time.Time}`
- [ ] 2.2 Create `internal/models/identity.go`: `Identity{ID uuid.UUID; UserID uuid.UUID (fk users, on delete cascade); Provider string; Subject string; Secret *string; CreatedAt, UpdatedAt time.Time}` with composite unique indexes `(provider, subject)` and `(user_id, provider)`
- [ ] 2.3 Create `internal/models/refresh_token.go`: `RefreshToken{ID uuid.UUID; UserID uuid.UUID; TokenHash []byte (sha-256, 32 bytes); ExpiresAt time.Time; RevokedAt *time.Time; ReplacedByID *uuid.UUID; UserAgent string; IPAtIssue string; CreatedAt time.Time}`; index on `(token_hash)` and `(user_id, revoked_at)`
- [ ] 2.4 Create `internal/models/verification_token.go` and `internal/models/password_reset_token.go` with `{ID; UserID; TokenHash; ExpiresAt; UsedAt *time.Time; CreatedAt}`, both indexed on `(token_hash)`
- [ ] 2.5 Create `internal/models/project.go`: `Project{ID uuid.UUID; UserID uuid.UUID (fk users on delete cascade, indexed); Title string; Thumbnail *string; CreatedAt, UpdatedAt time.Time}`; add a composite index on `(user_id, updated_at DESC, id DESC)` to support cursor pagination
- [ ] 2.6 Create `internal/models/canvas_element.go`: `CanvasElement{ID uuid.UUID; ProjectID uuid.UUID (fk projects on delete cascade, indexed); ElementData datatypes.JSON; CreatedAt, UpdatedAt time.Time}`
- [ ] 2.7 Create `internal/models/user_credits.go`: `UserCredits{UserID uuid.UUID (pk, fk users on delete cascade); Credits int (not null, default 1000); CreatedAt, UpdatedAt time.Time}`
- [ ] 2.8 Enable `citext` extension via a GORM migration step (`db.Exec("CREATE EXTENSION IF NOT EXISTS citext")`) executed before `AutoMigrate`; document the dependency in `backend/README.md`
- [ ] 2.9 Wire model registration in `cmd/server/main.go`: `db.RegisterModels(&models.User{}, &models.Identity{}, &models.RefreshToken{}, &models.VerificationToken{}, &models.PasswordResetToken{}, &models.Project{}, &models.CanvasElement{}, &models.UserCredits{})` before the `AutoMigrate` call

## 3. Auth primitives

- [ ] 3.1 Implement `internal/auth/password/argon2.go` wrapping `alexedwards/argon2id` with `Hash(plain string) (encoded string, error)` and `Verify(plain, encoded string) (ok bool, needsRehash bool, err error)`; expose tunable params as package constants
- [ ] 3.2 Implement `internal/auth/jwt/keys.go` providing a `KeyStore` that on construction loads the active RSA private key (`AUTH_JWT_PRIVATE_KEY_PATH`) + public key (`AUTH_JWT_PUBLIC_KEY_PATH`), computes a deterministic `kid = base64url(sha256(public-key-DER))[:22]`, and exposes `SigningKey() (*rsa.PrivateKey, kid string)` plus `PublicKeys() []PublicKey` (slice supports future rotation). When both paths are unset and `ENV=dev`, generate a 2048-bit RSA pair into `backend/.dev-keys/jwt.{key,pub}` (mode 0600 / 0644) on first run and reuse on subsequent runs; ensure `.dev-keys/` is gitignored
- [ ] 3.3 Implement `internal/auth/jwt/jwt.go`: `IssueAccess(userID uuid.UUID, email string, emailVerified bool) (token string, jti uuid.UUID, exp time.Time, err error)` using RS256 + the active key from `KeyStore` (header `kid` set), and `Verify(token string) (*Claims, error)` that resolves the verification key by `kid` against `KeyStore.PublicKeys()`, returning typed claims (`Sub`, `Email`, `EmailVerified`, `JTI`, `Exp`)
- [ ] 3.4 Implement `internal/auth/jwt/jwks.go` providing a `JWKS()` builder that emits the RFC 7517 JSON shape `{"keys":[{kty:"RSA", use:"sig", alg:"RS256", kid, n, e}, …]}` from `KeyStore.PublicKeys()`; expose `Handler(w, r)` that returns this JSON with `Cache-Control: public, max-age=300` and `Content-Type: application/json`
- [ ] 3.5 Implement `internal/auth/refresh/refresh.go`: `Issue(ctx, db, userID, ua, ip) (plainToken string, row *RefreshToken, err error)` (generates 32-byte random, stores sha256), `Rotate(ctx, db, plain, ua, ip) (newPlain string, newRow *RefreshToken, err error)` with reuse detection (if presented token is already revoked, revoke entire chain and return `ErrReuseDetected`), `Revoke(ctx, db, plain) error`, `RevokeAllForUser(ctx, db, userID) error`
- [ ] 3.6 Implement `internal/auth/csrf/csrf.go`: `IssueToken() (string, error)` (32-byte base64url), `Verify(headerValue, cookieValue string) bool` (constant-time)
- [ ] 3.7 Implement `internal/auth/ratelimit/ratelimit.go`: define `Limiter` interface with `Allow(ctx, key string) (allowed bool, retryAfter time.Duration)`; ship an in-memory implementation backed by `golang.org/x/time/rate` with per-key buckets, parameterized by `(rate, burst)`; include a background sweeper that drops buckets idle >1h
- [ ] 3.8 Implement `internal/auth/cookies/cookies.go` centralizing `SetAccess`, `SetRefresh`, `SetCSRF`, `ClearAll`, choosing `__Host-` prefix and `Secure`/`SameSite=Lax`/`Path` based on `cfg.AuthCookieSecure`; refresh cookie path = `/api/auth/refresh`; CSRF cookie `Max-Age = AUTH_ACCESS_TTL`

## 4. OIDC (Google)

- [ ] 4.1 Implement `internal/auth/oidc/google.go` initializing `oidc.NewProvider(ctx, "https://accounts.google.com")` at startup and building a verifier with `oidc.Config{ClientID: cfg.OIDCGoogleClientID}`
- [ ] 4.2 Implement `BuildAuthURL(state, codeChallenge, nonce string) string` using `oauth2.Config{ClientID, ClientSecret, RedirectURL, Endpoint: google.Endpoint, Scopes: ["openid","email","profile"]}`
- [ ] 4.3 Implement `ExchangeAndVerify(ctx, code, codeVerifier, expectedNonce string) (*GoogleClaims, error)` returning `{Sub, Email, EmailVerified}`; reject when `EmailVerified != true`
- [ ] 4.4 Implement `internal/auth/oidc/state.go` storing `{state, codeVerifier, nonce, next}` HMAC-signed (using `cfg.AuthOIDCStateSecret` — independent of the JWT signing keys) inside the short-lived `__Host-oidc-state` cookie; expiry 10 minutes

## 5. Email transport and templates

- [ ] 5.1 Define `internal/email/sender.go` interface `Sender interface { Send(ctx context.Context, to, subject, html, text string) error }`
- [ ] 5.2 Implement `internal/email/smtp.go` using `gopkg.in/gomail.v2`, honoring `SMTP_HOST/PORT/USERNAME/PASSWORD/FROM`; auth disabled when username is empty (matches Mailpit dev case)
- [ ] 5.3 Add `internal/email/templates/verify.go` and `reset.go` returning rendered `{subject, html, text}` from `text/template` literals; both must include `{{ .Link }}` and a "if this wasn't you, ignore this email" disclaimer
- [ ] 5.4 Add `mailpit` service to `docker-compose.dev.yml` (image `axllent/mailpit:latest`, ports `1025:1025` and `8025:8025`, no volumes); update `backend/README.md` to mention `http://localhost:8025` for inbox

## 6. Auth service + handlers

- [ ] 6.1 Implement `internal/auth/service/service.go` exposing the orchestration methods: `Register(ctx, email, password, ua, ip)`, `Login(ctx, email, password, ua, ip)`, `Refresh(ctx, plainRefresh, ua, ip)`, `Logout(ctx, plainRefresh)`, `RequestPasswordReset(ctx, email)`, `ResetPassword(ctx, token, newPassword)`, `RequestEmailVerification(ctx, userID)`, `VerifyEmail(ctx, token)`, `LoginViaGoogle(ctx, claims, ua, ip)`, `Me(ctx, userID)`; each returns a service-level error type that the handler layer maps to coarse HTTP error codes
- [ ] 6.2 Implement same-email merge rules in `Register` and `LoginViaGoogle` exactly as specified in design D8 (do **not** leak email existence on register conflicts; OIDC merge requires Google `email_verified=true`)
- [ ] 6.3 On every successful `Register`/`LoginViaGoogle` that creates a new user, insert a `user_credits` row with `Credits = 1000` in the same DB transaction
- [ ] 6.4 On every successful `Login`, detect outdated argon2 params via `Verify(..., needsRehash)` and silently re-hash the password identity with current params
- [ ] 6.5 On `ResetPassword` and on password change of any kind, revoke all refresh tokens for the user (`refresh.RevokeAllForUser`)
- [ ] 6.6 Implement handlers under `internal/auth/handlers/`: `register.go`, `login.go`, `logout.go`, `refresh.go`, `forgot_password.go`, `reset_password.go`, `verify_email.go`, `verify_email_resend.go`, `oidc_google_start.go`, `oidc_google_callback.go`, `me.go`, `jwks.go` — all returning the uniform error JSON shape `{error, message}`. The credential-issuing handlers (`register`, `login`, `refresh`, `oidc_google_callback`) MUST set the `csrf` cookie via `cookies.SetCSRF`; `me.go` MUST NOT set or rotate the `csrf` cookie; `jwks.go` MUST be unauthenticated and never set any cookie
- [ ] 6.7 Implement `internal/auth/middleware/require_auth.go` extracting the user from `__Host-access` cookie or `Authorization: Bearer`, calling `jwt.Verify` (which resolves the key by `kid` against the `KeyStore`), attaching `*models.User` (loaded from DB if not in claims sufficiently) to `gin.Context` under key `currentUser`; respond 401 with `{error:"unauthenticated"}` on failure

## 7. CSRF, CORS, rate-limit, and request-id middleware

- [ ] 7.1 Implement `internal/middleware/csrf.go`: enforce double-submit on unsafe methods **only** when request is cookie-authenticated (i.e., `__Host-access` cookie present and no `Authorization` header); compare `X-CSRF-Token` header to `csrf` cookie value with `subtle.ConstantTimeCompare`; respond 403 `{error:"csrf"}` on mismatch. The middleware MUST NOT set or rotate the `csrf` cookie itself — issuance is the responsibility of credential-issuing handlers (see §6.6)
- [ ] 7.2 Implement `internal/middleware/cors.go`: when `cfg.FrontendOrigin` is non-empty, set `Access-Control-Allow-Origin` to that exact value, `Allow-Credentials: true`, `Allow-Methods`, `Allow-Headers` (incl. `X-CSRF-Token`), and short-circuit `OPTIONS` with 204
- [ ] 7.3 Implement `internal/middleware/ratelimit.go`: factory that wires per-route limiters (login per-IP & per-email, register per-IP, forgot/resend per-IP & per-email, OIDC callback per-IP) using the `Limiter` interface; on deny, return 429 with `Retry-After` header and `{error:"rate_limited"}` body
- [ ] 7.4 Implement `internal/middleware/request_id.go`: read `X-Request-Id` or generate a UUID, attach to context, echo back in `X-Request-Id` response header, and include in every slog log entry for the request
- [ ] 7.5 Implement `internal/middleware/slog_access_log.go` replacing Gin's default logger with a slog-based access log that includes method, path, status, duration, request id, and (if available) user id

## 8. Business APIs

- [ ] 8.1 Implement `internal/business/projects/repo.go` (GORM) with `ListByUser(ctx, userID, limit int, cursor *Cursor) (rows []Project, nextCursor *Cursor, err error)` (cursor = `(updatedAt time.Time, id uuid.UUID)`; query is `user_id = ? AND (updated_at, id) < (?, ?) ORDER BY updated_at DESC, id DESC LIMIT ?`), `Get(id, userID)`, `Create(userID, title, thumbnail)`, `Update(id, userID, patch)`, `Delete(id, userID)`; every method enforces `user_id = current user` at the query level
- [ ] 8.2 Implement `internal/business/projects/cursor.go` with `EncodeCursor(updatedAt time.Time, id uuid.UUID) string` (base64url of `<rfc3339nano>|<uuid>`) and `DecodeCursor(s string) (Cursor, error)`; decode failures return a typed `ErrInvalidCursor`
- [ ] 8.3 Implement `internal/business/projects/handlers.go` exposing `GET /api/projects`, `POST /api/projects`, `GET /api/projects/:id`, `PATCH /api/projects/:id`, `DELETE /api/projects/:id`. The list handler MUST parse `limit` (default 20, clamp at 100, reject ≤0 with 400 `invalid_limit`) and `cursor` (decode via §8.2; on `ErrInvalidCursor` return 400 `invalid_cursor`); response body MUST be `{"items":[…],"nextCursor":"…"|null}`. The single-project endpoints MUST return `{id, user_id, title, thumbnail, created_at, updated_at}` matching the prior Supabase shape
- [ ] 8.4 Implement `internal/business/canvas_elements/repo.go` with `ListByProject(projectID, userID)` (joins through projects to enforce ownership), `ReplaceAllForProject(projectID, userID, elements []ElementInput)` matching the current "save all elements" pattern in `src/app/lovart/canvas/page.tsx`
- [ ] 8.5 Implement `internal/business/canvas_elements/handlers.go` exposing `GET /api/projects/:id/canvas-elements` and `PUT /api/projects/:id/canvas-elements`
- [ ] 8.6 Implement `internal/business/credits/repo.go` and `handlers.go` exposing `GET /api/credits` returning `{user_id, credits, created_at, updated_at}`
- [ ] 8.7 Apply `RequireAuth` + `CSRF` middleware to every `/api/projects`, `/api/credits`, `/api/projects/:id/canvas-elements` route

## 9. Router wiring

- [ ] 9.1 Extend `internal/httpserver/router.go` to mount route groups: `r.Group("/api/auth")` (no auth required except `/me`; the JWKS subroute is mounted explicitly without `RequireAuth`/`CSRF`), `r.Group("/api")` with `RequireAuth` + `CSRF` for business endpoints; apply `RequestID`, `slogAccessLog`, and conditional `CORS` globally
- [ ] 9.2 Mount the JWKS handler at `GET /api/auth/.well-known/jwks.json` ahead of any auth/CSRF middleware so it remains publicly reachable; ensure the route is exposed via the existing `/api/auth/*` prefix so the Next.js dev rewrite covers it without additional configuration
- [ ] 9.3 Register every handler from §6 and §8 on the router with explicit per-route rate-limit middleware where required
- [ ] 9.4 Add `GET /api/health` mirroring `/healthz` (so the same path works through the Next.js dev rewrite); both must return identical responses
- [ ] 9.5 Update `cmd/server/main.go` to construct the JWT `KeyStore`, `auth.Service`, `email.Sender`, `oidc.Verifier`, the rate-limit registry, and inject them into the router constructor

## 10. Frontend — remove Clerk and Supabase

- [ ] 10.1 `npm uninstall @clerk/backend @clerk/nextjs @supabase/supabase-js`; install `jose` (`npm install jose`); commit `package.json` + `package-lock.json`
- [ ] 10.2 Delete `src/hooks/useSupabase.ts`, `src/lib/supabase.ts`, `src/app/api/test-auth/route.ts`
- [ ] 10.3 Remove `ClerkProvider` from `src/app/layout.tsx`; wrap children with a new `AuthProvider` (added in §11)
- [ ] 10.4 Delete `CLERK_JWT_SETUP.md`; update `SETUP_GUIDE.md` and root `README.md` to remove every reference to Clerk and Supabase (replace with pointers to the new auth/data setup steps)
- [ ] 10.5 Delete root-level `supabase-schema.sql` and `add-user-credits.sql`; mention in commit message that they're preserved in git history and superseded by Go-side GORM models

## 11. Frontend — auth client + pages

- [ ] 11.1 Add `src/lib/auth/server.ts` with `getServerSession()` that reads the access cookie and verifies the JWT against the backend's JWKS using `jose.jwtVerify` with a module-scoped `createRemoteJWKSet(new URL(jwksUrl))` (target: `${BACKEND_INTERNAL_URL ?? 'http://localhost:8080'}/api/auth/.well-known/jwks.json`); the JWKS set's TTL is `AUTH_JWKS_CACHE_TTL` (default 300 s, parseable from env). Return `{ user } | null`. The module MUST NOT read any signing secret
- [ ] 11.2 Add `src/lib/auth/client.ts` with: `AuthProvider` React context, `useAuth()` hook that fetches `/api/auth/me` once on mount and exposes `{ user, loading, refresh, signOut }`, and a `signIn(email, password)`/`signUp(email, password)`/`signInWithGoogle()` helper set
- [ ] 11.3 Add `src/lib/api.ts` with `fetchWithCsrf` (reads `csrf` cookie via `document.cookie`, sets `X-CSRF-Token` header, `credentials: 'include'`; on missing CSRF cookie before a state-changing call, POST `/api/auth/refresh` then retry once; on refresh-401 surface to caller). Typed wrappers: `listProjects(opts?: {limit?: number; cursor?: string}) => Promise<{items: Project[]; nextCursor: string | null}>`, `getProject`, `createProject`, `updateProject`, `deleteProject`, `getCanvasElements`, `putCanvasElements`, `getCredits`
- [ ] 11.4 Create pages: `src/app/sign-in/page.tsx` (email+password form + "Sign in with Google" button), `src/app/sign-up/page.tsx`, `src/app/sign-in/forgot-password/page.tsx`, `src/app/sign-in/reset-password/page.tsx`, `src/app/sign-in/verify-email-sent/page.tsx`, `src/app/sign-in/verify-email/page.tsx` (calls `/api/auth/verify-email?token=…` then redirects), `src/app/oidc/callback/page.tsx` (handles status & redirect)
- [ ] 11.5 Replace `src/middleware.ts` with a route guard: when the request path matches `/lovart(/.*)?` and `__Host-access` (or `access`) cookie is missing or fails JWKS-based verification (uses the same `jose.createRemoteJWKSet` approach as §11.1), redirect to `/sign-in?next=<original>`; allow `/`, `/sign-in*`, `/sign-up*`, `/oidc/callback`, `/api/auth/*`, `/api/health`, static assets
- [ ] 11.6 Add `next.config.ts` rewrites: `/api/auth/:path*`, `/api/projects/:path*`, `/api/credits/:path*` → `http://localhost:8080/...` (dev only — guard with `process.env.NODE_ENV === 'development'`)

## 12. Frontend — refactor every Supabase/Clerk call site

- [x] 12.1 Refactor `src/app/lovart/page.tsx` and `src/app/lovart/projects/page.tsx`: replace `useSupabase()` queries with the paginated `listProjects()` from `src/lib/api.ts` (consume `items` + `nextCursor`, render a "Load more" affordance whenever `nextCursor` is non-null), plus `createProject()`/`deleteProject()`; replace `useUser()`/`useSession()` with `useAuth()`
- [x] 12.2 Refactor `src/app/lovart/canvas/page.tsx`: replace project + canvas-elements queries with `getProject(id)` + `getCanvasElements(id)` + `putCanvasElements(id, elements)`
- [x] 12.3 Refactor `src/app/lovart/user/page.tsx`: replace credits query with `getCredits()`; replace `UserButton`/`SignedIn`/`SignedOut` (if any) with `useAuth()`-driven UI; sign-out button calls `signOut()` which POSTs `/api/auth/logout` then `router.replace('/sign-in')`
- [x] 12.4 Update `src/app/api/generate-design/route.ts`, `src/app/api/generate-image/route.ts`, `src/app/api/generate-video/route.ts`, `src/app/api/video-status/route.ts` to call `getServerSession()` (from §11.1) instead of Clerk's `auth()`; respond 401 if null; otherwise pass `session.user.id` where the previous code passed `userId`
- [x] 12.5 Scan repo for any remaining `@clerk` or `@supabase` imports (`grep -r '@clerk' src/`, `grep -r '@supabase' src/`); all hits must be deleted or refactored

## 13. Frontend env + docs

- [x] 13.1 Update `SETUP_GUIDE.md` end-to-end: replace the Clerk/Supabase setup walkthrough with a "Start Postgres + Mailpit (`docker-compose up`), copy `backend/.env.example` to `backend/.env` (in dev the JWT keys auto-generate; set `AUTH_OIDC_STATE_SECRET` and Google OIDC credentials), `go run ./cmd/server`, `npm run dev`" flow
- [x] 13.2 Add a section explaining how to obtain Google OIDC client credentials (Google Cloud Console → APIs & Services → Credentials → OAuth client ID → Web application) and how to set the authorized redirect URI to `http://localhost:8080/api/auth/oidc/google/callback`
- [x] 13.3 Add a `.env.local.example` at repo root for the Next.js process documenting `BACKEND_INTERNAL_URL=http://localhost:8080` (used by `getServerSession()` to fetch JWKS) and `AUTH_JWKS_CACHE_TTL=300`. The file MUST NOT mention any signing secret
- [x] 13.4 Update root `README.md` "Tech stack" to drop Clerk/Supabase and add "Go backend (Gin + GORM), in-house auth with Google OIDC, RS256 JWT + JWKS"
- [x] 13.5 Document in `backend/README.md`: (a) where dev keys are auto-generated (`backend/.dev-keys/`); (b) how to generate a production keypair (`openssl genrsa -out jwt.key 2048 && openssl rsa -in jwt.key -pubout -out jwt.pub`); (c) the JWKS endpoint URL and its public, cacheable nature

## 14. Tests

- [x] 14.1 Write Go integration tests under `backend/internal/auth/service/service_test.go` covering: register-then-verify-then-login; register-existing-email-returns-generic-error; login-wrong-password; login-rate-limited; refresh-success; refresh-reuse-detected-revokes-chain; logout-revokes-refresh; OIDC-merge-into-verified-user; OIDC-creates-new-user-when-no-match; forgot-then-reset-revokes-all-refresh
- [x] 14.2 Write Go handler tests under `backend/internal/auth/handlers/*_test.go` using `httptest` covering: CSRF rejection on unsafe cookie-auth request; bearer-auth bypasses CSRF; 401 on missing/expired access; 429 with `Retry-After` on rate limit; `me` does not set `csrf` cookie; `login`/`refresh`/`register` do set `csrf` cookie
- [x] 14.3 Write Go handler tests under `backend/internal/auth/handlers/jwks_test.go` covering: JWKS responds 200 unauthenticated; emitted `kid` matches the `kid` header on a freshly issued access token; response includes `Cache-Control: public, max-age=300`
- [x] 14.4 Write Go integration tests under `backend/internal/business/.../*_test.go` covering: list-projects-only-returns-own; list-projects-cursor-pagination-roundtrip; list-projects-rejects-malformed-cursor (HTTP 400); list-projects-clamps-oversized-limit (≤100); cross-user-fetch-returns-404; canvas-elements-replace-roundtrip; credits-defaults-to-1000-on-user-creation
- [x] 14.5 Add a Mailpit-assisted manual test checklist under `backend/README.md` (capture the test plan from design.md "Migration Plan" step 4)

## 15. Verification

- [x] 15.1 `cd backend && go vet ./... && go build ./... && go test ./...` — all green
- [x] 15.2 `npm run lint && npm run build` — both succeed with no Clerk/Supabase references
- [x] 15.3 End-to-end smoke (run locally): register a user → verify email via Mailpit → log in → create project → reload → project visible → log out → "Sign in with Google" merges into the same account → forgot-password flow rotates refresh tokens (old refresh returns 401) → `/lovart` is blocked when not logged in (redirect to `/sign-in?next=/lovart`); also confirm `curl -s http://localhost:8080/api/auth/.well-known/jwks.json | jq '.keys[0].kid'` matches the `kid` header in a freshly minted access JWT
- [x] 15.4 `grep -r '@clerk\|@supabase\|ClerkProvider\|useSupabase\|createClerkSupabaseClient\|auth\\.jwt\|AUTH_JWT_SECRET' src/ package.json README.md SETUP_GUIDE.md` returns no matches (CLERK_JWT_SETUP.md is deleted, and no symmetric JWT secret remains anywhere in the frontend)
- [x] 15.5 `openspec validate add-auth-with-password-and-oidc --strict` passes
