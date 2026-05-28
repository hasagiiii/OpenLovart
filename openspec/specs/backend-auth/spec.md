## ADDED Requirements

### Requirement: User registration with email and password

The system SHALL accept user registration via `POST /api/auth/register` with `{email, password}`. On success it MUST create a `users` row with `email_verified_at = NULL` and a corresponding `identities` row (`provider="password"`, `subject=<user-id>`, `secret=<argon2id-encoded>`), enqueue a verification email, and return HTTP 201 with the newly issued access and refresh cookies. The system MUST reject weak passwords (fewer than 8 characters) and malformed emails. The system MUST NOT reveal whether an email is already registered.

#### Scenario: Successful registration

- **WHEN** a client POSTs `{"email":"alice@example.com","password":"correct horse"}` and no `users` row exists for that email
- **THEN** the system creates a `users` row, creates a `password` identity, sends a verification email containing a tokenized link to `EMAIL_VERIFY_BASE_URL`, sets `__Host-access` + `__Host-refresh` + `csrf` cookies, creates a `user_credits` row with `Credits=1000`, and returns HTTP 201 with body `{"id":"<uuid>","email":"alice@example.com","email_verified":false}`

#### Scenario: Duplicate email

- **WHEN** a client POSTs an email that is already registered (regardless of verification state)
- **THEN** the system returns HTTP 409 with body `{"error":"email_unavailable","message":"This email cannot be used."}` — identical wording whether the existing record is verified or not, to avoid disclosure

#### Scenario: Weak password

- **WHEN** a client POSTs `{"email":"...","password":"abc"}`
- **THEN** the system returns HTTP 400 with body `{"error":"weak_password","message":"Password must be at least 8 characters."}` and does not create any rows

### Requirement: Email verification

The system SHALL issue verification tokens that are valid for 24 hours, stored in `verification_tokens` as `sha256(token)`. The system SHALL expose `GET /api/auth/verify-email?token=<plain>` that validates the token and sets `users.email_verified_at = now()` on success. The system SHALL expose `POST /api/auth/verify-email/resend` rate-limited per IP and per email; the response MUST be HTTP 204 regardless of whether the email exists.

#### Scenario: Token consumed successfully

- **WHEN** a verified-not-yet user opens `/api/auth/verify-email?token=<valid-unused-token>`
- **THEN** the system sets `email_verified_at = now()` on the target user, marks the token row `used_at = now()`, and HTTP-redirects 302 to `<EMAIL_VERIFY_BASE_URL>/?status=ok`

#### Scenario: Expired token

- **WHEN** a client opens `/api/auth/verify-email` with a token whose `expires_at` is in the past
- **THEN** the system 302-redirects to `<EMAIL_VERIFY_BASE_URL>/?status=expired` without modifying any user row

#### Scenario: Already-used token

- **WHEN** a client opens `/api/auth/verify-email` with a token whose `used_at` is non-null
- **THEN** the system 302-redirects to `<EMAIL_VERIFY_BASE_URL>/?status=already_used`

#### Scenario: Resend for nonexistent email returns 204

- **WHEN** a client POSTs `/api/auth/verify-email/resend` with an email that does not exist
- **THEN** the system returns HTTP 204 and sends no email (and the response timing is comparable to the existing-email branch within practical bounds)

### Requirement: Password login

The system SHALL accept `POST /api/auth/login` with `{email, password}`. On match, the system MUST issue access and refresh cookies and return HTTP 200 with the user shape. On mismatch (unknown email, wrong password, or no `password` identity for the user), the system MUST return HTTP 401 with a generic error code, performing a constant-time-comparable amount of work to avoid timing-based email enumeration.

#### Scenario: Successful login

- **WHEN** a client POSTs valid credentials for a verified user
- **THEN** the system issues a 15-minute access JWT (in `__Host-access` cookie + body), a 30-day opaque refresh (in `__Host-refresh` cookie scoped to `/api/auth/refresh`), a fresh CSRF cookie, and returns HTTP 200 with `{"id":"<uuid>","email":"...","email_verified":true,"providers":["password"]}`

#### Scenario: Login for unverified user is allowed but flagged

- **WHEN** an unverified user logs in with correct credentials
- **THEN** the system returns HTTP 200 with `email_verified:false`; the access JWT's `email_verified` claim is also `false`

#### Scenario: Wrong password

- **WHEN** a client POSTs an incorrect password for an existing user
- **THEN** the system returns HTTP 401 with body `{"error":"invalid_credentials","message":"Email or password is incorrect."}` and does not reveal whether the email exists

#### Scenario: Unknown email

- **WHEN** a client POSTs credentials for an email that does not exist
- **THEN** the system performs a dummy argon2 verification and returns HTTP 401 with the same `invalid_credentials` body and a response time comparable to the wrong-password branch within practical bounds

#### Scenario: Silent argon2 parameter upgrade

- **WHEN** a user's stored argon2 parameters are older than the current package defaults and they log in successfully
- **THEN** the system re-hashes the password with current parameters and updates the `identities.secret` value in the same request lifecycle

### Requirement: Session tokens and refresh

The system SHALL issue JWT access tokens (RS256, 15 min TTL, signed with the backend's RSA private key, carrying a `kid` header that names the active signing key) and opaque refresh tokens (32 bytes random, stored as SHA-256 hash, 30 day TTL). The system SHALL expose `POST /api/auth/refresh` that, given a valid refresh cookie, rotates the refresh token (issues a new one and marks the old one revoked), issues a new access token, and re-issues a fresh `csrf` cookie. Reuse of an already-revoked refresh token MUST revoke every token in its chain and reject the request.

#### Scenario: Successful refresh rotates

- **WHEN** a client POSTs `/api/auth/refresh` with a valid, unrevoked, unexpired refresh cookie
- **THEN** the system creates a new `refresh_tokens` row, marks the previous row `revoked_at = now()` with `replaced_by_id = <new row>`, sets new `__Host-access`, `__Host-refresh`, and `csrf` cookies, and returns HTTP 200 with the user shape

#### Scenario: Refresh reuse detection

- **WHEN** a client POSTs `/api/auth/refresh` with a refresh token whose corresponding row already has `revoked_at != NULL`
- **THEN** the system revokes every row in the chain (following `replaced_by_id` forward) that is still active, clears the cookies, and returns HTTP 401 with `{"error":"refresh_reuse","message":"Session was invalidated. Please sign in again."}`

#### Scenario: Expired refresh

- **WHEN** the refresh row's `expires_at` is in the past
- **THEN** the system returns HTTP 401 with `{"error":"refresh_expired","message":"Session expired. Please sign in again."}` and clears the cookies

#### Scenario: Access token is RS256 with a kid

- **WHEN** any access token is issued (via login, register, refresh, or OIDC callback)
- **THEN** the JWT header has `alg=RS256` and a `kid` whose value matches one of the keys advertised by `GET /api/auth/.well-known/jwks.json`

### Requirement: JWKS endpoint for public-key distribution

The system SHALL expose `GET /api/auth/.well-known/jwks.json` returning a JWK Set containing the RSA public key(s) used to sign access tokens. The response MUST be unauthenticated, MUST include `Cache-Control: public, max-age=300`, and MUST conform to RFC 7517: an object with a top-level `keys` array, each entry containing at minimum `kty="RSA"`, `use="sig"`, `alg="RS256"`, `kid`, `n`, `e`. The endpoint MUST be reachable through the same `/api/auth/*` prefix used by other auth endpoints (so the Next.js dev rewrite covers it without additional configuration).

#### Scenario: JWKS exposes the active signing key

- **WHEN** the backend has loaded an RSA key pair on startup and a client GETs `/api/auth/.well-known/jwks.json`
- **THEN** the response is HTTP 200 with `Content-Type: application/json`, body `{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"<kid>","n":"<base64url>","e":"AQAB"}]}`, and the `kid` matches the `kid` header on access tokens issued by the same process

#### Scenario: JWKS is publicly cacheable

- **WHEN** any client GETs `/api/auth/.well-known/jwks.json`
- **THEN** the response includes `Cache-Control: public, max-age=300` and does not require authentication

#### Scenario: Missing private key fails closed in non-dev

- **WHEN** the process starts with `ENV != dev` and no `AUTH_JWT_PRIVATE_KEY_PATH` configured (or the file is unreadable)
- **THEN** startup fails with a fatal log line; the server does not bind a port

### Requirement: Logout

The system SHALL expose `POST /api/auth/logout` that, when given a valid refresh cookie, revokes the corresponding refresh row, clears the access/refresh/csrf cookies, and returns HTTP 204. The endpoint MUST succeed (return 204) even when no valid refresh cookie is present.

#### Scenario: Authenticated logout

- **WHEN** an authenticated client POSTs `/api/auth/logout`
- **THEN** the matching `refresh_tokens` row is marked `revoked_at = now()`, all three cookies are cleared (Max-Age=0), and the response is HTTP 204

#### Scenario: Logout without a session is idempotent

- **WHEN** a client POSTs `/api/auth/logout` with no cookies
- **THEN** the response is HTTP 204 with cookie-clear headers

### Requirement: Password reset

The system SHALL expose `POST /api/auth/forgot-password` accepting `{email}` and always responding HTTP 204, internally enqueueing a reset email (with a 1-hour tokenized link) only when a matching verified user exists. The system SHALL expose `POST /api/auth/reset-password` accepting `{token, new_password}`. On success it MUST update the password identity (creating it if the user only had OIDC), mark the token used, and revoke every refresh token belonging to that user.

#### Scenario: Forgot password for unknown email is silent

- **WHEN** a client POSTs `/api/auth/forgot-password` with an email that does not exist
- **THEN** the system returns HTTP 204 and sends no email

#### Scenario: Successful reset revokes refresh tokens

- **WHEN** a user successfully consumes a reset token
- **THEN** the system updates the user's password identity, marks the token row `used_at = now()`, calls `RevokeAllForUser` to revoke every active refresh row for that user, and returns HTTP 204

#### Scenario: OIDC-only user adds a password via reset

- **WHEN** a user with only an `oidc:google` identity completes a forgot/reset flow
- **THEN** the system creates a new `password` identity for the user (with the new password) instead of returning an error, and the user can subsequently log in with email + password

#### Scenario: Reset with expired token

- **WHEN** a client POSTs `/api/auth/reset-password` with a token whose `expires_at` is in the past
- **THEN** the system returns HTTP 400 with `{"error":"invalid_token","message":"This reset link is no longer valid."}`

### Requirement: Google OIDC login

The system SHALL expose `GET /api/auth/oidc/google/start` that builds a Google authorization URL (with `state`, PKCE `code_challenge`, and `nonce`), persists the corresponding `code_verifier`/`nonce`/`next` HMAC-signed in a 10-minute cookie, and 302-redirects to Google. The system SHALL expose `GET /api/auth/oidc/google/callback` that validates `state`, exchanges the code for an ID token using `coreos/go-oidc`, verifies the ID token signature/issuer/audience/nonce, requires `email_verified == true`, performs same-email merge (D8 in design.md), and issues the standard cookies on success.

#### Scenario: First-time Google sign-in creates user

- **WHEN** a client completes `/api/auth/oidc/google/callback` for an email that has no existing `users` row, and Google's ID token has `email_verified=true`
- **THEN** the system creates a `users` row with `email_verified_at = now()`, an `identities` row `(provider="oidc:google", subject=<google_sub>, secret=NULL)`, a `user_credits` row with `Credits=1000`, sets the standard cookies, and 302-redirects to `<frontend>/oidc/callback?status=ok&next=<next-or-default>`

#### Scenario: Google sign-in merges into existing verified user

- **WHEN** the email already maps to a `users` row with `email_verified_at != NULL`
- **THEN** the system inserts an `identities` row for `(provider="oidc:google", subject=<google_sub>)` (or does nothing if it already exists) and logs the user in

#### Scenario: Google sign-in for unverified existing user verifies the account

- **WHEN** the email already maps to a `users` row with `email_verified_at = NULL`
- **THEN** the system sets `email_verified_at = now()` on that user, inserts the OIDC identity, and logs the user in

#### Scenario: Google asserts unverified email — rejected

- **WHEN** Google's ID token contains `email_verified=false`
- **THEN** the system 302-redirects to `<frontend>/oidc/callback?status=email_unverified` without creating or modifying any user

#### Scenario: State mismatch — rejected

- **WHEN** the `state` query parameter does not match the value stored in the `oidc-state` cookie
- **THEN** the system returns HTTP 400 with `{"error":"oidc_state_mismatch","message":"Authentication request could not be verified."}`

### Requirement: Current-user endpoint

The system SHALL expose `GET /api/auth/me` that returns the current authenticated user's `{id, email, email_verified, providers, created_at}`. The endpoint MUST NOT issue or rotate the `csrf` cookie (CSRF cookie issuance is the responsibility of credential-issuing endpoints — see the CSRF requirement below). Unauthenticated requests MUST return HTTP 401.

#### Scenario: Authenticated me

- **WHEN** an authenticated client GETs `/api/auth/me`
- **THEN** the system returns HTTP 200 with `{"id":"<uuid>","email":"...","email_verified":true,"providers":["password","google"],"created_at":"..."}` and **no** `Set-Cookie` header for `csrf`

#### Scenario: Unauthenticated me

- **WHEN** an unauthenticated client GETs `/api/auth/me`
- **THEN** the system returns HTTP 401 with `{"error":"unauthenticated"}` and no Set-Cookie

### Requirement: Cookie configuration

The system SHALL set authentication cookies with the following attributes:
- Access cookie: `__Host-access` when `AUTH_COOKIE_SECURE=true`, otherwise `access`; `HttpOnly`; `SameSite=Lax`; `Path=/`; `Secure` matches `AUTH_COOKIE_SECURE`; `Max-Age` matches `AUTH_ACCESS_TTL`.
- Refresh cookie: `__Host-refresh` or `refresh`; `HttpOnly`; `SameSite=Lax`; `Path=/api/auth/refresh`; `Secure` matches `AUTH_COOKIE_SECURE`; `Max-Age` matches `AUTH_REFRESH_TTL`.
- CSRF cookie: `csrf`; `HttpOnly=false` (so the client can read it); `SameSite=Lax`; `Path=/`; `Secure` matches `AUTH_COOKIE_SECURE`; `Max-Age` matches `AUTH_ACCESS_TTL`.

#### Scenario: Secure cookies in production mode

- **WHEN** the server starts with `AUTH_COOKIE_SECURE=true`
- **THEN** every set-cookie header for the three cookies includes `Secure` and uses the `__Host-` prefix where applicable

#### Scenario: Insecure cookies in development

- **WHEN** the server starts with `AUTH_COOKIE_SECURE=false`
- **THEN** set-cookie headers omit `Secure` and use plain (`access`, `refresh`, `csrf`) cookie names

### Requirement: CSRF protection for cookie-authenticated requests

The system SHALL enforce double-submit CSRF on every state-changing request (`POST`, `PUT`, `PATCH`, `DELETE`) that authenticates via the access cookie. The `X-CSRF-Token` request header MUST equal the `csrf` cookie value (constant-time compare). Requests authenticated via `Authorization: Bearer` MUST be exempt from CSRF enforcement.

The `csrf` cookie MUST be issued **only** by endpoints that issue or refresh credentials: `POST /api/auth/login`, `POST /api/auth/register`, `POST /api/auth/refresh`, and the OIDC callback. Other authenticated endpoints (including `GET /api/auth/me` and every business endpoint) MUST NOT rotate or re-issue the `csrf` cookie. The cookie's `Max-Age` MUST equal `AUTH_ACCESS_TTL` so that its lifecycle tracks the access token.

#### Scenario: CSRF token matches

- **WHEN** a cookie-authenticated client POSTs to `/api/projects` with `X-CSRF-Token` equal to its `csrf` cookie
- **THEN** the system processes the request normally

#### Scenario: CSRF token missing on cookie-auth request

- **WHEN** a cookie-authenticated client POSTs to `/api/projects` without `X-CSRF-Token`
- **THEN** the system returns HTTP 403 with `{"error":"csrf","message":"Request rejected by CSRF protection."}` and does not invoke the handler

#### Scenario: CSRF token mismatch on cookie-auth request

- **WHEN** a cookie-authenticated client POSTs with `X-CSRF-Token` different from the `csrf` cookie
- **THEN** the system returns HTTP 403 `{"error":"csrf","message":"Request rejected by CSRF protection."}`

#### Scenario: Bearer-authenticated request bypasses CSRF

- **WHEN** a client POSTs to `/api/projects` with `Authorization: Bearer <jwt>` and no cookies
- **THEN** the system processes the request normally regardless of any CSRF header

#### Scenario: CSRF cookie is set on credential events

- **WHEN** a client successfully completes `POST /api/auth/login`, `POST /api/auth/register`, `POST /api/auth/refresh`, or `GET /api/auth/oidc/google/callback`
- **THEN** the response includes a `Set-Cookie: csrf=<32-byte-base64url>` header with `Max-Age` equal to `AUTH_ACCESS_TTL`, `Path=/`, `SameSite=Lax`, `HttpOnly=false`, and `Secure` mirroring `AUTH_COOKIE_SECURE`

#### Scenario: CSRF cookie is not rotated by non-credential endpoints

- **WHEN** an authenticated client GETs `/api/auth/me` or any `/api/projects`/`/api/credits`/`/api/projects/:id/canvas-elements` endpoint
- **THEN** the response does not contain a `Set-Cookie: csrf=…` header (the existing cookie persists unchanged)

### Requirement: Rate limiting on authentication endpoints

The system SHALL apply per-route rate limiting using an in-memory token-bucket limiter behind a `Limiter` interface. Limits MUST be configurable via the env vars listed in `proposal.md`. Default ceilings:
- `/api/auth/login`: 10/min per IP **and** 5/min per email (whichever is exceeded first)
- `/api/auth/register`: 20/hour per IP
- `/api/auth/forgot-password` and `/api/auth/verify-email/resend`: 5/hour per IP **and** per email
- `/api/auth/oidc/google/callback`: 30/min per IP

On exceeding any applicable limit the system MUST return HTTP 429 with header `Retry-After: <seconds>` and body `{"error":"rate_limited","message":"Too many requests. Please try again later."}`.

#### Scenario: Login rate limit per IP

- **WHEN** an IP submits 11 `POST /api/auth/login` requests within 60 seconds
- **THEN** the 11th request returns HTTP 429 with a `Retry-After` header and the `rate_limited` body, and the request never reaches the login handler

#### Scenario: Login rate limit per email

- **WHEN** a single email receives 6 login attempts within 60 seconds (from any combination of IPs)
- **THEN** the 6th attempt returns HTTP 429 with `Retry-After`

### Requirement: Same-email account merge rules

The system SHALL maintain at most one `users` row per distinct email and SHALL link multiple authentication providers to that single row via `identities`. Merging across providers MUST require email verification: the merge target's email MUST be verified, OR the incoming provider MUST itself assert email verification (Google's `email_verified=true`).

#### Scenario: Password user later signs in with Google (verified existing user)

- **WHEN** a verified user previously created via password completes a Google sign-in for the same email
- **THEN** the system inserts an `identities` row `(provider="oidc:google", subject=<google_sub>)` linked to the existing user (provided no other user already owns that `(provider,subject)` pair) and logs the user in; the user thereafter has both `password` and `oidc:google` providers

#### Scenario: Google user later registers with password — conflict

- **WHEN** a user previously created via Google attempts to register the same email with a password
- **THEN** the system returns HTTP 409 `{"error":"email_unavailable","message":"This email cannot be used."}` — the user must instead use forgot-password to add a password identity (covered by the OIDC-only-user-adds-password scenario above)
