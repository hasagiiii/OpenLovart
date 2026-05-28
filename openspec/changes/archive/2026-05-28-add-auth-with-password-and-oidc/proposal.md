## Why

OpenLovart currently delegates identity to **Clerk** (browser-issued JWTs) and all data access to **Supabase** (PostgREST + RLS using `auth.jwt()->>'sub'`). Phase 1 (`add-go-backend-skeleton`) landed a runnable Go service but did not yet replace either dependency. This change makes the self-hosted stack real:

1. **Authentication**: Replace Clerk with an in-house implementation that supports both email + password and Google OIDC sign-in, with email verification, password reset, and same-email account merging.
2. **Session model**: Issue short-lived JWT access tokens (self-contained) paired with long-lived opaque refresh tokens tracked in the database (revocable, per-device).
3. **Data ownership**: Move every read/write of `projects`, `canvas_elements`, and `user_credits` from "browser → Supabase" to "browser → Go backend → PostgreSQL". This is the only way to truly delete Clerk, because the existing Supabase-direct path *requires* a Clerk-issued JWT for RLS.
4. **Cleanup**: Remove `@clerk/*` and `@supabase/*` dependencies, the Clerk-flavored env vars, the `useSupabase` hook, the `auth.jwt()->>'sub'`-based RLS policies, and all Clerk references in docs.

Without this change, "self-hosted" is a half-truth: data still lives in Supabase and identity still lives in Clerk.

## What Changes

### Backend (Go)

- **New `internal/auth/` package** implementing:
  - Password hashing via `argon2id` (`alexedwards/argon2id`).
  - Google OIDC login via `coreos/go-oidc/v3` + `golang.org/x/oauth2` (state + PKCE, ID-token verification, JWKS auto-refresh).
  - Account model with same-email merge: one `users` row per verified email; `identities` rows for `password` and `oidc:google` providers.
  - Email verification flow (signed token in URL → endpoint marks `email_verified_at`).
  - Password reset flow (forgot-password → tokenized email link → reset endpoint).
  - Session model: **JWT access token** (HS256 from a backend-only secret, 15 min TTL, in `Authorization: Bearer …` *and* `__Host-access` cookie) + **opaque refresh token** (32-byte random, HMAC-stored in DB, 30 day TTL, sliding renewal, `__Host-refresh` cookie path-scoped to `/api/auth/refresh`).
  - CSRF protection: double-submit cookie pattern (`csrf` cookie + `X-CSRF-Token` header) enforced on every unsafe request that authenticates via cookies.
  - Rate limiting on `/api/auth/login`, `/api/auth/register`, `/api/auth/forgot-password`, `/api/auth/verify-email/resend`, and `/api/auth/oidc/google/callback` (in-memory token-bucket per IP + per identifier; pluggable interface so we can move to Redis later).
- **New `internal/email/` package** with an `Sender` interface and an SMTP implementation (`net/smtp` + `gopkg.in/gomail.v2`-style API). Templates live alongside (HTML + plain text for verification and reset emails).
- **New `internal/business/` package** owning `projects`, `canvas_elements`, `user_credits` CRUD via GORM repositories + service layer.
- **New models**: `User`, `Identity`, `RefreshToken`, `VerificationToken`, `PasswordResetToken`, `Project`, `CanvasElement`, `UserCredits`. All registered with `db.RegisterModels(...)` in `main.go` and migrated via `AutoMigrate`.
- **New HTTP routes** (Gin):
  - `POST /api/auth/register`, `POST /api/auth/login`, `POST /api/auth/logout`, `POST /api/auth/refresh`
  - `POST /api/auth/forgot-password`, `POST /api/auth/reset-password`
  - `POST /api/auth/verify-email/resend`, `GET /api/auth/verify-email` (token in query)
  - `GET /api/auth/oidc/google/start`, `GET /api/auth/oidc/google/callback`
  - `GET /api/auth/me`
  - `GET/POST/PATCH/DELETE /api/projects`, `GET/PUT /api/projects/:id/canvas-elements`, `GET /api/credits`
- **New middleware**: `RequireAuth` (extracts user from cookie or bearer, rejects with 401), CORS (configurable origin), CSRF, rate-limit.
- **New config keys** (backend-only): `FRONTEND_ORIGIN`, `AUTH_JWT_SECRET`, `AUTH_ACCESS_TTL`, `AUTH_REFRESH_TTL`, `AUTH_COOKIE_DOMAIN`, `AUTH_COOKIE_SECURE`, `OIDC_GOOGLE_CLIENT_ID`, `OIDC_GOOGLE_CLIENT_SECRET`, `OIDC_GOOGLE_REDIRECT_URL`, `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_FROM`, `EMAIL_VERIFY_BASE_URL`, `PASSWORD_RESET_BASE_URL`, `RATE_LIMIT_LOGIN_PER_MIN`, `RATE_LIMIT_REGISTER_PER_HOUR`.

### Frontend (Next.js)

- **Remove** `@clerk/nextjs`, `@clerk/backend`, `@supabase/supabase-js`, the `ClerkProvider` in `src/app/layout.tsx`, `src/hooks/useSupabase.ts`, `src/lib/supabase.ts`, and `src/app/api/test-auth/route.ts`.
- **Add** `src/lib/auth.ts` (client + server helpers around the cookie session, including a `fetchWithCsrf` wrapper) and `src/lib/api.ts` (typed wrappers over the Go backend's `/api/projects`, `/api/credits`, `/api/auth/me`, `/api/auth/login`, etc., all relative URLs so the same-origin cookie applies; in dev a Next.js rewrite proxies `/api/*` to `http://localhost:8080`).
- **Add** pages: `/sign-in`, `/sign-up`, `/sign-in/forgot-password`, `/sign-in/reset-password`, `/sign-in/verify-email-sent`, `/sign-in/verify-email` (consumes the token), `/oidc/callback`.
- **Replace** `useSession`/`useUser`/`UserButton`/`SignedIn`/etc. with an in-house `useAuth` hook backed by `/api/auth/me`, plus a session-aware layout component.
- **Replace** every Supabase call site (`src/app/lovart/projects/page.tsx`, `src/app/lovart/canvas/page.tsx`, `src/app/lovart/user/page.tsx`) with the new `api.ts` helpers.
- **Middleware** in `src/middleware.ts` becomes a route guard: unauthenticated visitors hitting `/lovart/*` are redirected to `/sign-in?next=…`. Public routes: `/`, `/sign-in*`, `/sign-up*`, `/oidc/callback`, `/api/health`.
- **Update** `src/app/api/generate-design/route.ts`, `src/app/api/generate-image/route.ts`, `src/app/api/generate-video/route.ts`, `src/app/api/video-status/route.ts` to read `userId` from the cookie session (via shared server helper) instead of `auth()` from Clerk.

### Database / Schema

- **Drop**: `supabase-schema.sql` (preserved in git history; superseded by GORM models). All `auth.jwt()`-based RLS policies are abandoned because access now goes exclusively through the Go backend, which enforces ownership in service code. RLS is **off** for the new tables (the Go backend is the only client and uses an application-level role).
- **Repurpose**: `add-user-credits.sql` becomes a one-shot script kept only as documentation — same intent is now expressed as a GORM model + initial-credits service rule (`1000` on first sign-up).
- **One-time migration script** (`backend/cmd/import-supabase/` or a SQL file under `openspec/changes/.../migrations/`): copies existing `user_credits`, `projects`, `canvas_elements` rows from Supabase to the new PostgreSQL. Because Clerk `user_id` (string like `user_2abc...`) is being replaced by a new `uuid` PK, the script also creates a placeholder `users` row per legacy Clerk id, marked `legacy_clerk_id` and inviting the user to reset password / link Google on first new login. (Self-hosted dev with no real users: the script is documented but its only execution target in this phase is local dev seed data.)

### Infrastructure

- `docker-compose.dev.yml` gains a `mailpit` service (lightweight SMTP catcher, port 1025 SMTP + 8025 web UI) so email flows are exercisable locally without a real provider.

## Capabilities

### New Capabilities

- **`backend-auth`** — Self-hosted identity. Owns users, identities, sessions, password hashing, OIDC, email verification, password reset, CSRF, and rate limiting.
- **`backend-business-api`** — Server-side ownership of `projects`, `canvas_elements`, `user_credits`. Replaces the Supabase-direct path.
- **`frontend-auth-and-data`** — Browser-side login/registration UI, cookie-session client, and refactor of every page to call the Go backend instead of Supabase/Clerk.

### Modified Capabilities

- **`backend-runtime`** — Adds CORS middleware (origin = `FRONTEND_ORIGIN`, credentials = true), rate-limit middleware, mounts the new auth + business route groups, and serves `/api/health` in addition to `/healthz` (so the frontend's same-origin probe works through the dev proxy).
- **`backend-persistence`** — Model registry is now non-empty: the auth and business models are registered at startup. AutoMigrate becomes a real migration on first run.

### Removed Capabilities

_None at the spec level._ Clerk and Supabase-direct were never first-class OpenSpec capabilities (they were implementation details of the pre-skeleton era). Their removal is captured in the **Impact** section below.

## Impact

- **Affected code (frontend, deletions or rewrites)**:
  - `src/app/layout.tsx` (drop `ClerkProvider`)
  - `src/middleware.ts` (Clerk-comment → real route guard)
  - `src/hooks/useSupabase.ts` **(delete)**
  - `src/lib/supabase.ts` **(delete)**
  - `src/app/api/test-auth/route.ts` **(delete)**
  - `src/app/lovart/page.tsx`, `src/app/lovart/projects/page.tsx`, `src/app/lovart/canvas/page.tsx`, `src/app/lovart/user/page.tsx` (replace Supabase calls)
  - `src/app/api/generate-*` routes (swap Clerk `auth()` → new cookie session helper)
  - `package.json` (remove `@clerk/backend`, `@clerk/nextjs`, `@supabase/supabase-js`)
  - `CLERK_JWT_SETUP.md`, `SETUP_GUIDE.md`, `README.md` (Clerk/Supabase sections rewritten or removed)
  - **New** files under `src/app/(auth)/*`, `src/lib/auth.ts`, `src/lib/api.ts`, `src/components/auth/*`.

- **Affected code (backend, new)**:
  - `backend/internal/auth/` (handlers, service, password, jwt, oidc, csrf, ratelimit packages)
  - `backend/internal/email/` (sender interface + SMTP impl + templates)
  - `backend/internal/business/` (projects, credits, canvas-elements handlers/service/repo)
  - `backend/internal/models/` (`user.go`, `identity.go`, `refresh_token.go`, `verification_token.go`, `password_reset_token.go`, `project.go`, `canvas_element.go`, `user_credits.go`)
  - `backend/internal/middleware/` (auth, cors, csrf, ratelimit, request_id)
  - `backend/cmd/server/main.go` (wire new routes, register models, init oidc verifier, init email sender)

- **Database**: New tables `users`, `identities`, `refresh_tokens`, `verification_tokens`, `password_reset_tokens`, `projects` (rebuilt with `uuid user_id` FK), `canvas_elements`, `user_credits` (rebuilt with `uuid user_id` PK). RLS off. `supabase-schema.sql` removed (preserved in git).

- **Environment variables**:
  - **Removed (browser)**: `NEXT_PUBLIC_SUPABASE_URL`, `NEXT_PUBLIC_SUPABASE_ANON_KEY`, `NEXT_PUBLIC_CLERK_PUBLISHABLE_KEY`, `NEXT_PUBLIC_CLERK_*`.
  - **Removed (server)**: `CLERK_SECRET_KEY`, `SUPABASE_SERVICE_ROLE_KEY` (if present).
  - **Added (backend)**: see the full list under *Backend* above.
  - **Added (frontend)**: `NEXT_PUBLIC_BACKEND_URL` (used for OIDC redirect display + production absolute URLs).

- **Dependencies**:
  - **Backend (added)**: `github.com/alexedwards/argon2id`, `github.com/coreos/go-oidc/v3`, `golang.org/x/oauth2`, `github.com/golang-jwt/jwt/v5`, `golang.org/x/time/rate`, `gopkg.in/gomail.v2`.
  - **Frontend (removed)**: `@clerk/backend`, `@clerk/nextjs`, `@supabase/supabase-js`.

- **Infrastructure**: `docker-compose.dev.yml` gains `mailpit` service.

- **Breaking**: This change is breaking for any existing Clerk-authenticated session (sessions become invalid; users must register a new account or reset password). Acceptable because the project has no production user base yet.

- **Non-goals** (explicitly out of scope; punted to future changes):
  - Additional OIDC providers (GitHub, Apple, Microsoft) — schema and `identities.provider` column leave room, but only Google is wired now.
  - Multi-factor authentication.
  - Magic-link / passwordless email login.
  - Admin dashboard for user management.
  - Production deployment (TLS termination, secret-manager integration, Redis-backed rate limiting, persistent SMTP provider).
  - Replacing AutoMigrate with a dedicated migration tool.
