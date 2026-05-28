## ADDED Requirements

### Requirement: Projects CRUD

The system SHALL expose the following endpoints for the authenticated user's projects, scoped strictly by the current user's `id`:
- `GET /api/projects?limit=<n>&cursor=<opaque>` — list the user's projects, newest first by `(updated_at DESC, id DESC)`, cursor-paginated.
- `POST /api/projects` — create a project.
- `GET /api/projects/:id` — fetch one project owned by the user.
- `PATCH /api/projects/:id` — partial update (title, thumbnail).
- `DELETE /api/projects/:id` — delete the project (cascade deletes its `canvas_elements`).

Every endpoint MUST require authentication (cookie or bearer) and CSRF for unsafe methods. Ownership enforcement MUST be at the query level (every SQL statement includes `user_id = <current>` so that even a programming mistake cannot leak across users).

The list endpoint's `limit` MUST default to 20, MUST be capped at 100 (values >100 are clamped to 100), and MUST reject non-positive integers with HTTP 400. The cursor MUST be a base64url-encoded value of `<rfc3339-updated-at>|<uuid>` from the last item of the previous page; cursors that fail to decode or that don't refer to a row owned by the caller MUST result in HTTP 400 with `{"error":"invalid_cursor"}` (NOT a silent fall-through to the first page). The response shape MUST be `{"items": [...], "nextCursor": "<opaque>" | null}` where `nextCursor` is `null` when there are no more rows.

#### Scenario: List returns only the caller's projects, paginated

- **WHEN** an authenticated user GETs `/api/projects` (no query params)
- **THEN** the response is HTTP 200 with body `{"items":[{id, user_id, title, thumbnail, created_at, updated_at}, …], "nextCursor": "<opaque>" | null}`, items contain at most 20 projects whose `user_id` matches the caller, ordered by `(updated_at DESC, id DESC)`, and `nextCursor` is non-null iff at least one further row exists for this user

#### Scenario: List honors limit and cursor

- **WHEN** an authenticated user owns 25 projects and GETs `/api/projects?limit=10`, then GETs `/api/projects?limit=10&cursor=<nextCursor from page 1>`, then again with the next cursor
- **THEN** the three responses together return exactly the user's 25 projects with no overlap and no gaps; the third response's `nextCursor` is `null`

#### Scenario: List clamps oversized limit

- **WHEN** an authenticated user GETs `/api/projects?limit=500`
- **THEN** the response contains at most 100 items and is otherwise identical to a `limit=100` request

#### Scenario: List rejects malformed cursor

- **WHEN** an authenticated user GETs `/api/projects?cursor=not-a-valid-cursor`
- **THEN** the response is HTTP 400 with `{"error":"invalid_cursor","message":"Cursor is not valid."}` and no items are returned

#### Scenario: Create persists the project

- **WHEN** an authenticated user POSTs `{"title":"My Deck","thumbnail":null}` to `/api/projects`
- **THEN** the response is HTTP 201 with the full project shape, `user_id` set to the caller, server-generated `id`/`created_at`/`updated_at`, and a follow-up GET returns the same row

#### Scenario: Cross-user fetch is indistinguishable from missing

- **WHEN** user A GETs `/api/projects/<id-owned-by-user-B>`
- **THEN** the response is HTTP 404 with `{"error":"not_found"}` — the system MUST NOT return 403, MUST NOT leak the existence of the row

#### Scenario: Update with empty patch is a no-op success

- **WHEN** an authenticated user PATCHes `/api/projects/:id` with `{}`
- **THEN** the response is HTTP 200 with the unchanged project shape

#### Scenario: Delete cascades to canvas elements

- **WHEN** an authenticated user DELETEs a project that has canvas elements
- **THEN** the response is HTTP 204 and a subsequent SELECT against `canvas_elements WHERE project_id = <deleted>` returns zero rows

### Requirement: Canvas elements replace-all

The system SHALL expose:
- `GET /api/projects/:id/canvas-elements` — list elements for a project owned by the caller.
- `PUT /api/projects/:id/canvas-elements` — replace the project's element set atomically with the request body's array.

Both endpoints MUST resolve project ownership via a join (`projects.user_id = current_user`) and MUST return 404 when the project is not owned by the caller.

#### Scenario: Get returns ordered elements

- **WHEN** an authenticated user GETs `/api/projects/<own-project>/canvas-elements`
- **THEN** the response is HTTP 200 with a JSON array of `{id, project_id, element_data, created_at, updated_at}` ordered by `created_at` ascending

#### Scenario: Put atomically replaces elements

- **WHEN** an authenticated user PUTs an array of element bodies to `/api/projects/<own-project>/canvas-elements`
- **THEN** the system runs a single transaction that (1) deletes all existing elements for that project and (2) inserts the new set; on commit, a subsequent GET returns exactly the new set; on any failure, the original set is preserved

#### Scenario: Put against non-owned project

- **WHEN** user A PUTs to `/api/projects/<id-owned-by-user-B>/canvas-elements`
- **THEN** the response is HTTP 404, the request body is not parsed against the database, and user B's elements are unchanged

### Requirement: Credits read endpoint

The system SHALL expose `GET /api/credits` returning the caller's `user_credits` row in the shape `{user_id, credits, created_at, updated_at}`. The endpoint MUST never return another user's credits, MUST never return 404 (a row is created at registration, so it always exists for an authenticated user), and MUST NOT permit writes via this path (write paths are introduced in a later change when business logic spends credits).

#### Scenario: Authenticated read

- **WHEN** an authenticated user GETs `/api/credits`
- **THEN** the response is HTTP 200 with `{"user_id":"<uuid>","credits":<int>,"created_at":"...","updated_at":"..."}`

#### Scenario: Unauthenticated read

- **WHEN** an unauthenticated client GETs `/api/credits`
- **THEN** the response is HTTP 401 with `{"error":"unauthenticated"}`

### Requirement: Initial credit allocation on user creation

The system SHALL insert a `user_credits` row with `credits=1000` in the same database transaction that creates each new `users` row (via password registration or first-time OIDC login). The insertion MUST be a `ON CONFLICT DO NOTHING` against `(user_id)` so that retries are safe.

#### Scenario: New user has credits after registration

- **WHEN** a user is created via `POST /api/auth/register`
- **THEN** a subsequent `GET /api/credits` for that user returns `credits = 1000`

#### Scenario: New user has credits after first Google sign-in

- **WHEN** a user is created via `/api/auth/oidc/google/callback`
- **THEN** a subsequent `GET /api/credits` for that user returns `credits = 1000`

### Requirement: Response shape compatibility with prior Supabase clients

The system SHALL return JSON shapes for individual `projects`, `canvas_elements`, and `user_credits` rows that match the Supabase row shape currently consumed by the frontend, with one exception: `user_id` is now a UUID string (was an opaque Clerk id string). Field names and casing MUST match (`user_id`, `project_id`, `element_data`, `created_at`, `updated_at`). The list endpoint for projects deliberately diverges from Supabase: it returns `{items, nextCursor}` instead of a bare array (see Projects CRUD); the frontend `listProjects()` wrapper is responsible for unwrapping `items` so downstream UI components continue to consume an array.

#### Scenario: Frontend rehydrates without field rename

- **WHEN** the refactored `src/lib/api.ts` calls `GET /api/projects`, unwraps `items`, and feeds an individual project into the existing `ProjectCard` component
- **THEN** the component renders without changes to its prop reads (`title`, `thumbnail`, `updated_at`, `id`)
