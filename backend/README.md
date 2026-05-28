# OpenLovart Backend

Go (Gin + GORM) backend for OpenLovart. Provides:

- Email + password authentication with email verification, password reset,
  and refresh-token rotation with reuse detection.
- Google OIDC sign-in (PKCE + signed `state` cookie).
- RS256-signed JWT access tokens distributed via a public JWKS endpoint.
- Business APIs: `projects`, `canvas-elements`, `credits`.

## Prerequisites

- **Go** 1.22 or newer (developed with Go 1.25).
- **Docker / Docker Compose** for the bundled PostgreSQL and Mailpit.
  Skip if you point `DATABASE_URL` / `SMTP_*` at services you already manage.
- **PostgreSQL** 13 or newer (must allow `CREATE EXTENSION citext, pgcrypto`
  on first boot — the bundled compose file's superuser already does).

## Quick start

From the repository root:

```bash
# 1. Start PostgreSQL + Mailpit.
docker compose -f docker-compose.dev.yml up -d

# 2. Configure backend.
cp backend/.env.example backend/.env
# Set AUTH_OIDC_STATE_SECRET (>= 32 random bytes); JWT keys auto-generate in dev.

# 3. Run the backend.
cd backend
go mod tidy   # first time only
go run ./cmd/server

# 4. Verify in another terminal.
curl -i http://localhost:8080/healthz
# → HTTP/1.1 200 OK   {"status":"ok","db":"ok"}
curl -s http://localhost:8080/api/auth/.well-known/jwks.json | jq .
# → {"keys":[{"kty":"RSA","kid":"...","alg":"RS256",...}]}
```

To stop the backend, send `Ctrl-C`. The process performs a graceful shutdown
(closes the HTTP listener, drains in-flight requests up to 10 s, closes the
database pool) and exits with code 0.

## Configuration

Configuration is loaded by [`viper`](https://github.com/spf13/viper) from the
process environment, with optional fallback to a `backend/.env` file.
**Environment variables always override `.env`.** Defaults apply when neither
source provides a value.

The complete list of variables (with inline documentation) lives in
[`backend/.env.example`](./.env.example). Highlights:

| Variable                     | Default                                                                          | Notes                                                                 |
| ---------------------------- | -------------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ENV`                        | `dev`                                                                            | `dev` enables text logs + Gin debug; anything else uses JSON + release. |
| `HTTP_ADDR`                  | `:8080`                                                                          | Listen address (`[host]:port`).                                       |
| `DATABASE_URL`               | `postgres://openlovart:openlovart@localhost:5432/openlovart?sslmode=disable`     | GORM DSN. `sslmode=disable` is dev-only.                              |
| `AUTH_JWT_PRIVATE_KEY_PATH`  | (auto in dev)                                                                    | RS256 private key. **Required** in prod.                              |
| `AUTH_JWT_PUBLIC_KEY_PATH`   | (auto in dev)                                                                    | RS256 public key. **Required** in prod.                               |
| `AUTH_OIDC_STATE_SECRET`     | (none)                                                                           | HMAC secret for OIDC `state` cookie. **Required** in prod, ≥32 bytes. |
| `AUTH_COOKIE_SECURE`         | `false`                                                                          | **Must** be `true` in prod (validated).                               |
| `OIDC_GOOGLE_CLIENT_ID`/`_SECRET`/`_REDIRECT_URL` | (empty)                                                                          | Optional. Empty disables Google sign-in (password login still works). |
| `SMTP_HOST` / `SMTP_PORT`    | `localhost` / `1025`                                                             | Defaults target the bundled Mailpit.                                  |

## Auth — JWT signing keys (RS256)

Access tokens are signed with **RS256**. There is **no shared symmetric
secret** anywhere — relying parties (the Next.js frontend, any other
service) verify tokens by fetching the public JWKS endpoint:

```
GET /api/auth/.well-known/jwks.json
Cache-Control: public, max-age=300
```

The JWKS endpoint:

- is **public** (no authentication, no CSRF, no rate limiting);
- emits a stable `kid` per key, derived as
  `base64url(sha256(PKIX-DER-SubjectPublicKeyInfo))[:22]`;
- the same `kid` appears in the JWT header of every issued access token,
  so consumers can pick the right key.

### Dev mode — automatic key generation

In dev (`ENV=dev`) leave both `AUTH_JWT_PRIVATE_KEY_PATH` and
`AUTH_JWT_PUBLIC_KEY_PATH` blank. On first run the backend generates a
fresh 2048-bit RSA keypair and writes it to:

```
backend/.dev-keys/jwt.key   (mode 0600 — private key, PKCS#8 PEM)
backend/.dev-keys/jwt.pub   (mode 0644 — public key, PKIX PEM)
```

`backend/.dev-keys/` is gitignored. Subsequent runs reuse the same files,
so issued JWTs survive backend restarts. Delete the directory to rotate
the dev key.

### Production — supply your own keypair

In any non-dev `ENV` value the backend **refuses to start** unless both
key paths are set, exist on disk, and are readable. Generate a keypair
out-of-band and mount it as a secret:

```bash
openssl genrsa -out /etc/openlovart/jwt.key 2048
openssl rsa -in /etc/openlovart/jwt.key -pubout -out /etc/openlovart/jwt.pub
chmod 600 /etc/openlovart/jwt.key
```

Then set:

```env
ENV=production
AUTH_JWT_PRIVATE_KEY_PATH=/etc/openlovart/jwt.key
AUTH_JWT_PUBLIC_KEY_PATH=/etc/openlovart/jwt.pub
AUTH_OIDC_STATE_SECRET=<openssl rand -base64 48>
AUTH_COOKIE_SECURE=true
AUTH_COOKIE_DOMAIN=your.domain
FRONTEND_ORIGIN=https://your.frontend.tld
```

To rotate, generate a new keypair, deploy alongside the old one (the JWKS
endpoint can serve multiple keys; clients pick by `kid`), wait for the
old access tokens' TTL to expire (`AUTH_ACCESS_TTL`, default 15m), then
remove the old key files.

## Auth — Google OIDC

To enable Google sign-in, in [Google Cloud Console](https://console.cloud.google.com/):

1. APIs & Services → Credentials → Create credentials → **OAuth client ID** → **Web application**.
2. Under **Authorized redirect URIs** add (for local dev):
   ```
   http://localhost:8080/api/auth/oidc/google/callback
   ```
   Add your production callback URL alongside it as needed.
3. Copy the resulting Client ID / Client secret into `backend/.env`:
   ```env
   OIDC_GOOGLE_CLIENT_ID=...
   OIDC_GOOGLE_CLIENT_SECRET=...
   OIDC_GOOGLE_REDIRECT_URL=http://localhost:8080/api/auth/oidc/google/callback
   ```

The backend signs the OIDC `state` cookie with `AUTH_OIDC_STATE_SECRET`,
which is **independent** of the JWT signing key (rotating one must not
require rotating the other).

## HTTP endpoints

| Method | Path                                            | Auth     | Description                                                  |
| ------ | ----------------------------------------------- | -------- | ------------------------------------------------------------ |
| GET    | `/healthz`, `/api/health`                       | public   | Liveness + DB ping.                                          |
| GET    | `/api/auth/.well-known/jwks.json`               | public   | RSA public keys for verifying access tokens.                 |
| POST   | `/api/auth/register`                            | public   | Email + password registration. Sends verification mail.      |
| GET    | `/api/auth/verify-email`                        | public   | Consumes a verification token (link from email).             |
| POST   | `/api/auth/verify-email/resend`                 | session  | Re-issues a verification email.                              |
| POST   | `/api/auth/login`                               | public   | Issues `__Host-access`, `__Host-refresh`, `csrf` cookies.    |
| POST   | `/api/auth/refresh`                             | refresh  | Rotates the refresh token; reuse → revoke entire chain.      |
| POST   | `/api/auth/logout`                              | session  | Revokes the current refresh token + clears cookies.          |
| POST   | `/api/auth/forgot-password`                     | public   | Anti-enumeration: 200 regardless of email existence.         |
| POST   | `/api/auth/reset-password`                      | public   | Consumes reset token; revokes all refresh tokens for user.   |
| GET    | `/api/auth/oidc/google/start`                   | public   | Top-level redirect to Google with PKCE + signed state.       |
| GET    | `/api/auth/oidc/google/callback`                | public   | Validates state + PKCE; merges into existing verified user.  |
| GET    | `/api/auth/me`                                  | session  | Current user `{id, email, email_verified}`.                  |
| GET    | `/api/projects?limit=&cursor=`                  | session  | Cursor-paginated list (default 20, max 100).                 |
| POST   | `/api/projects`                                 | session  | Creates a project owned by the caller.                       |
| GET    | `/api/projects/:id`                             | session  | Fetch project (404 if cross-user).                           |
| PATCH  | `/api/projects/:id`                             | session  | Update title / thumbnail.                                    |
| DELETE | `/api/projects/:id`                             | session  | Hard-delete; cascades canvas elements.                       |
| GET    | `/api/projects/:id/canvas-elements`             | session  | List canvas elements (entire set).                           |
| PUT    | `/api/projects/:id/canvas-elements`             | session  | Replace canvas elements (full set, `{elements:[…]}`).        |
| GET    | `/api/credits`                                  | session  | Current credit balance (auto-provisioned at 1000).           |

`session` = `__Host-access` JWT cookie present and valid + (for unsafe
methods) double-submit `X-CSRF-Token` matches the `csrf` cookie. Bearer
tokens (`Authorization: Bearer …`) bypass CSRF (cross-origin clients).

## Local mail capture (Mailpit)

The bundled `docker-compose.dev.yml` brings up
[Mailpit](https://github.com/axllent/mailpit) so the email flows can be
exercised end-to-end:

- SMTP listener: `localhost:1025` (matches the default `SMTP_HOST` / `SMTP_PORT`).
- Web inbox: <http://localhost:8025> — open in a browser to read captured
  messages, follow verification / reset links. No auth, **dev only**.

## Mailpit-assisted manual test checklist

Run after any auth-related change. All steps assume the Quick start is
running (backend on `:8080`, Next.js on `:3000`, Mailpit on `:8025`).

- [ ] **Register & verify**
  - `POST /api/auth/register` with a fresh email → response 200.
  - Open Mailpit → click verification link → backend redirects to the
    Next.js verification confirmation page.
  - `GET /api/auth/me` now returns `email_verified: true`.
- [ ] **Anti-enumeration**
  - Register the same email again → response is 200 (silent success);
    Mailpit shows **no** new mail.
- [ ] **Login → CSRF on first state change**
  - `POST /api/auth/login` → response sets `__Host-access`,
    `__Host-refresh`, **and** `csrf` cookies.
  - Issue an unsafe call without `X-CSRF-Token` → 403.
  - Repeat with `X-CSRF-Token: <csrf cookie value>` → 200.
- [ ] **Refresh rotation + reuse detection**
  - `POST /api/auth/refresh` once → new refresh cookie; old one no longer works.
  - Replay the **old** refresh token → 401 **and** all refresh tokens
    for that user are revoked (next legitimate refresh also 401).
- [ ] **Logout**
  - `POST /api/auth/logout` → cookies cleared, `/api/auth/me` returns 401.
- [ ] **Forgot password**
  - `POST /api/auth/forgot-password` (existing email) → 200; Mailpit
    shows reset email.
  - `POST /api/auth/forgot-password` (unknown email) → 200; Mailpit
    shows **no** new mail.
  - `POST /api/auth/reset-password` with a valid token → all existing
    refresh tokens for the user are revoked.
- [ ] **OIDC merge**
  - With Google OIDC configured, click "Sign in with Google" using the
    same email as a verified password user → response merges into the
    existing user (no duplicate row in `users`).
- [ ] **JWKS / kid coherence**
  - `curl -s http://localhost:8080/api/auth/.well-known/jwks.json | jq '.keys[0].kid'`
    matches the `kid` header of a freshly minted access JWT (decode the
    cookie at <https://jwt.io>).

## Project layout

```
backend/
├── cmd/server/main.go             # Process entry: load → log → db → wire deps → http
├── internal/
│   ├── auth/
│   │   ├── cookies/               # __Host- prefix + cookie-name centralization
│   │   ├── csrf/                  # double-submit verification helpers
│   │   ├── handlers/              # one file per endpoint (login, register, refresh, …)
│   │   ├── jwt/                   # RS256 signing, keys, JWKS publication
│   │   ├── middleware/            # RequireAuth (access-cookie + bearer)
│   │   ├── oidc/                  # Google verifier + state cookie signer
│   │   ├── password/              # argon2id hash + verify + needsRehash
│   │   ├── ratelimit/             # in-memory token-bucket per IP/email
│   │   ├── refresh/               # opaque token + sha256 hash + reuse detection
│   │   └── service/               # core auth orchestration (depended on by handlers)
│   ├── business/
│   │   ├── canvas_elements/
│   │   ├── credits/
│   │   └── projects/              # cursor pagination, owner-scoped queries
│   ├── config/                    # viper-backed Config struct
│   ├── db/                        # GORM open/close + AutoMigrate registry + ext setup
│   ├── email/                     # SMTP sender + text/html templates
│   ├── httpserver/                # Gin engine, middleware wiring, router (Deps)
│   ├── logging/                   # log/slog setup (text in dev, JSON in prod)
│   ├── middleware/                # CSRF, CORS, RateLimit, RequestID, slog access log
│   └── models/                    # GORM models (User, Identity, RefreshToken, …)
├── .dev-keys/                     # auto-generated dev RSA key (gitignored)
├── .env.example
├── go.mod
└── README.md (this file)
```

## Migrations: GORM AutoMigrate

We use **GORM `AutoMigrate`** as the schema-management tool. On every
startup the backend:

1. Runs `CREATE EXTENSION IF NOT EXISTS "citext"` and
   `CREATE EXTENSION IF NOT EXISTS "pgcrypto"`.
   - `citext` powers the case-insensitive `users.email` column.
   - `pgcrypto` provides `gen_random_uuid()` so the `default:gen_random_uuid()`
     GORM tag works on every primary key.
   The role used by `DATABASE_URL` therefore needs `CREATE EXTENSION`
   privilege the first time the backend boots against a fresh database
   (the bundled `docker-compose.dev.yml` superuser already satisfies this).
2. Registers the GORM models in `cmd/server/main.go`: `User`, `Identity`,
   `RefreshToken`, `VerificationToken`, `PasswordResetToken`, `Project`,
   `CanvasElement`, `UserCredits`.
3. Calls `db.AutoMigrate(registeredModels...)`.

### Known limitations of AutoMigrate

GORM's `AutoMigrate` is **non-destructive** by design:

- ✅ Creates new tables, columns, indexes, foreign keys.
- ✅ Adds missing constraints when safe to do so.
- ❌ Does **not** drop columns, drop tables, rename columns, change a
  column's type in lossy ways, or backfill data.

If a future schema change requires a destructive operation (rename a
column, remove a field, migrate data into a new shape), perform that
step manually against the database (or via a one-off SQL script
committed to the repo) before the AutoMigrate-compatible portion runs.
Document any such operation in the relevant OpenSpec change.

## Verifying changes locally

```bash
cd backend
go vet ./...
go build ./...
go test ./...
```

All three must succeed with no output / all tests passing before merging.
