## ADDED Requirements

### Requirement: Removal of Clerk and Supabase from the frontend

The frontend (the Next.js application in `src/`) SHALL contain zero references to `@clerk/*` packages, the `ClerkProvider`, Clerk hooks (`useUser`, `useSession`, `useAuth` from Clerk, `UserButton`, `SignedIn`, `SignedOut`), or to `@supabase/supabase-js`. The `package.json` MUST NOT list `@clerk/backend`, `@clerk/nextjs`, or `@supabase/supabase-js` as dependencies. Setup documentation MUST NOT instruct the developer to obtain Clerk or Supabase credentials.

#### Scenario: Static check after migration

- **WHEN** a developer runs `grep -r '@clerk\|@supabase\|ClerkProvider\|useSupabase\|createClerkSupabaseClient' src/ package.json README.md SETUP_GUIDE.md`
- **THEN** the command produces no matches (and the `CLERK_JWT_SETUP.md` file has been deleted)

#### Scenario: Lockfile reflects removal

- **WHEN** `npm install` is run after the migration
- **THEN** `package-lock.json` does not contain `@clerk/backend`, `@clerk/nextjs`, or `@supabase/supabase-js` entries

### Requirement: Cookie-based session client

The frontend SHALL authenticate to the Go backend exclusively via the `__Host-access` / `access` cookie issued by the backend. All client-side fetches against `/api/*` MUST use `credentials: 'include'`. State-changing fetches MUST attach the `X-CSRF-Token` header read from the `csrf` cookie. A single `fetchWithCsrf` helper in `src/lib/api.ts` MUST encapsulate both behaviors so that handlers do not duplicate this logic.

When `fetchWithCsrf` is invoked for a state-changing request and the `csrf` cookie is absent (typical after a long idle period where the access cookie has also expired), the helper MUST first POST to `/api/auth/refresh` to re-issue all three cookies, then retry the original request once. If the refresh call also fails (HTTP 401), `fetchWithCsrf` MUST surface the 401 to the caller without further retries; the caller's error handler is responsible for routing the user to `/sign-in`.

#### Scenario: GET inherits the cookie

- **WHEN** an authenticated page calls `listProjects()` (which internally hits `GET /api/projects`)
- **THEN** the request is sent with `credentials: 'include'`, the session cookie is attached automatically, and the response is parsed into the expected typed shape

#### Scenario: POST attaches CSRF

- **WHEN** the frontend calls `createProject({title})` (POST)
- **THEN** the request includes `credentials: 'include'`, the `X-CSRF-Token` header is set from `document.cookie`, and the request succeeds with HTTP 201

#### Scenario: Missing CSRF cookie triggers refresh-then-retry

- **WHEN** `fetchWithCsrf` cannot find a `csrf` cookie before a state-changing call
- **THEN** the helper first POSTs `/api/auth/refresh`; on success it reads the newly-set `csrf` cookie and retries the original request once; on refresh failure (HTTP 401) it returns the 401 to the caller without retrying further

### Requirement: Authentication pages

The frontend SHALL provide the following routes:
- `/sign-in` — email + password form and a "Sign in with Google" button.
- `/sign-up` — registration form; on success shows a "verification email sent" confirmation.
- `/sign-in/forgot-password` — email-entry form that POSTs to `/api/auth/forgot-password`.
- `/sign-in/reset-password` — accepts `?token=` from the URL, POSTs new password to `/api/auth/reset-password`.
- `/sign-in/verify-email-sent` — informational page shown after registration.
- `/sign-in/verify-email` — accepts `?token=` from the URL, calls `GET /api/auth/verify-email`, displays the resulting status.
- `/oidc/callback` — accepts `?status=` from the backend's redirect, displays success or error.

These pages MUST be accessible without authentication. The forms MUST surface server-side error codes as user-friendly text in the page's locale (English in this phase).

#### Scenario: Sign-in success navigates to next

- **WHEN** a user submits valid credentials on `/sign-in?next=/lovart/projects`
- **THEN** the page calls `POST /api/auth/login`, on HTTP 200 reads `next` from the URL, and uses `router.replace` to navigate to `/lovart/projects`

#### Scenario: Sign-up shows verification-sent confirmation

- **WHEN** a user successfully submits the sign-up form
- **THEN** the page redirects to `/sign-in/verify-email-sent` showing the email address used and an instruction to check the inbox

#### Scenario: OIDC start is a top-level navigation

- **WHEN** a user clicks "Sign in with Google" on `/sign-in`
- **THEN** the page performs a full-page navigation (`window.location.href = '/api/auth/oidc/google/start?next=…'`) rather than an XHR, so that the cookies set by the backend on the callback persist

### Requirement: Route guard middleware

`src/middleware.ts` SHALL enforce authentication for `/lovart` and any sub-path. Unauthenticated requests MUST be redirected to `/sign-in?next=<original-path>`. The middleware MUST allow the following paths without authentication: `/`, `/sign-in*`, `/sign-up*`, `/oidc/callback`, `/api/auth/*`, `/api/health`, and static assets. The check MUST be performed by verifying the access JWT against the backend's JWKS (fetched from `/api/auth/.well-known/jwks.json` via `jose.createRemoteJWKSet`, in-process cached for `AUTH_JWKS_CACHE_TTL`); a missing or invalid token MUST trigger redirect, not an error page. The middleware MUST NOT possess or read any signing secret.

#### Scenario: Unauthenticated visit to /lovart

- **WHEN** a request to `/lovart/projects` arrives without a valid access cookie
- **THEN** the middleware returns a 307 redirect to `/sign-in?next=%2Flovart%2Fprojects`

#### Scenario: Authenticated visit passes through

- **WHEN** a request to `/lovart/canvas/<id>` arrives with a valid (signature OK against JWKS, not expired) access cookie
- **THEN** the middleware allows the request to proceed to the page handler

#### Scenario: Public routes unaffected

- **WHEN** a request to `/`, `/sign-in`, `/sign-up`, `/oidc/callback`, `/api/auth/login`, or `/api/health` arrives
- **THEN** the middleware allows it through regardless of cookie state

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

#### Scenario: Existing Next API routes use the helper

- **WHEN** any of `src/app/api/generate-design`, `src/app/api/generate-image`, `src/app/api/generate-video`, `src/app/api/video-status` is invoked
- **THEN** the route handler calls `getServerSession()` and responds HTTP 401 if it returns null; otherwise it uses `session.user.id` for any downstream attribution that previously used Clerk's `userId`

### Requirement: Business data access through the Go backend

Every read or write of `projects`, `canvas_elements`, or `user_credits` SHALL be performed via the typed wrappers in `src/lib/api.ts` (which call the Go backend). The frontend MUST NOT instantiate a Supabase client. The pages `src/app/lovart/page.tsx`, `src/app/lovart/projects/page.tsx`, `src/app/lovart/canvas/page.tsx`, and `src/app/lovart/user/page.tsx` MUST be refactored to use these wrappers.

The `listProjects` wrapper MUST expose cursor pagination: its signature MUST accept an optional `{ limit?: number; cursor?: string }` argument, MUST hit `GET /api/projects?limit=…&cursor=…`, and MUST return `{ items: Project[]; nextCursor: string | null }`. The projects list page MUST drive paging via this shape (e.g., a "Load more" button that re-invokes `listProjects({ cursor: nextCursor })` and concatenates `items`); it MUST NOT assume the response is a bare array.

#### Scenario: Project list page paginates

- **WHEN** an authenticated user opens `/lovart/projects` and the user owns more than `limit` projects
- **THEN** the page initially renders the first page (calling `listProjects()` and reading `items`); a "Load more" affordance becomes visible whenever `nextCursor` is non-null; clicking it calls `listProjects({ cursor: nextCursor })` and appends the returned `items`; the affordance hides when `nextCursor` is `null`

#### Scenario: Canvas save

- **WHEN** the user triggers a save on `/lovart/canvas/<id>`
- **THEN** the page calls `putCanvasElements(id, elements)` (which hits `PUT /api/projects/:id/canvas-elements`), receives HTTP 200, and updates local UI state from the response

#### Scenario: Credits display

- **WHEN** the user opens `/lovart/user`
- **THEN** the page calls `getCredits()` (which hits `GET /api/credits`) and renders the numeric value from the response's `credits` field

### Requirement: Development proxy for backend routes

In development (`NODE_ENV === 'development'`), `next.config.ts` SHALL rewrite `/api/auth/:path*`, `/api/projects/:path*`, and `/api/credits/:path*` to `http://localhost:8080/api/auth/:path*`, `http://localhost:8080/api/projects/:path*`, and `http://localhost:8080/api/credits/:path*` respectively. The rewrite MUST NOT apply to `/api/generate-*` or `/api/video-status` so that those Next-served routes remain functional.

#### Scenario: Auth call goes to Go backend

- **WHEN** the browser POSTs to `/api/auth/login` during `npm run dev`
- **THEN** the request reaches the Go backend on port 8080 (verifiable via the backend's access log) and the browser treats the cookies returned by the Go backend as same-origin

#### Scenario: Generate routes stay on Next

- **WHEN** the browser POSTs to `/api/generate-image` during `npm run dev`
- **THEN** the request is handled by `src/app/api/generate-image/route.ts` inside the Next process (no rewrite to port 8080)
