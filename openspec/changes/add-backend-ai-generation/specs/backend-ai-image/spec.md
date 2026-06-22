## ADDED Requirements

### Requirement: Asynchronous image generation submit

The system SHALL expose `POST /api/ai/images` under the authenticated,
CSRF-protected business route group. The request body MUST accept a typed core
of `prompt` (required, non-empty string), optional `size`, optional `n`
(number of images, default 1), and optional `reference_image` (base64 or data
URI), plus an optional `provider_options` object for passthrough vendor
parameters. On a valid request the backend MUST create a job, return HTTP 202
with `{ "request_id": <id>, "status": "IN_QUEUE" }`, and begin generation
asynchronously. A missing or empty `prompt` MUST yield HTTP 400
`{"error":"invalid_request"}`.

#### Scenario: Submit returns a request id

- **WHEN** an authenticated client POSTs `{"prompt":"a sunset over mountains"}`
- **THEN** the backend responds HTTP 202 with body `{"request_id":"<id>","status":"IN_QUEUE"}` and the job is owned by the session user

#### Scenario: Missing prompt rejected

- **WHEN** an authenticated client POSTs a body with no `prompt` or an empty `prompt`
- **THEN** the backend responds HTTP 400 `{"error":"invalid_request"}` and no job is created

#### Scenario: Unauthenticated submit rejected

- **WHEN** a client without a valid session cookie POSTs to `/api/ai/images`
- **THEN** the backend responds HTTP 401 `{"error":"unauthenticated"}` and no job is created

### Requirement: Image job status

The system SHALL expose `GET /api/ai/images/:id/status` returning the job's
current status as one of `IN_QUEUE`, `IN_PROGRESS`, `COMPLETED`, or `FAILED`
(fal-shaped values). A `FAILED` status MUST include a human-readable `error`
field. A status request for an id the session user does not own MUST be
indistinguishable from a missing id (HTTP 404 `{"error":"not_found"}`).

#### Scenario: Status transitions to completed

- **WHEN** an authenticated owner GETs `/api/ai/images/:id/status` after generation finishes
- **THEN** the response is HTTP 200 with `{"status":"COMPLETED", ...}`

#### Scenario: Failed job reports an error

- **WHEN** generation fails for a job and its owner GETs the status
- **THEN** the response is HTTP 200 with `{"status":"FAILED","error":<message>}`

#### Scenario: Other user's job is not found

- **WHEN** an authenticated user GETs the status of a job owned by a different user, or of an unknown id
- **THEN** the response is HTTP 404 `{"error":"not_found"}`

### Requirement: Image job result

The system SHALL expose `GET /api/ai/images/:id` returning the final result for
a `COMPLETED` job as `{ "images": [ { "url": <string>, "width": <int>,
"height": <int>, "content_type": <string> }, ... ] }`. Requesting the result of
a job that is not yet `COMPLETED` MUST yield HTTP 409
`{"error":"not_ready","status":<current-status>}`. Ownership rules match the
status endpoint (non-owner / unknown id → HTTP 404).

#### Scenario: Result returns image urls

- **WHEN** an authenticated owner GETs `/api/ai/images/:id` for a completed job
- **THEN** the response is HTTP 200 with an `images` array whose entries each carry a usable `url`

#### Scenario: Result before completion

- **WHEN** an authenticated owner GETs the result of a job still `IN_QUEUE` or `IN_PROGRESS`
- **THEN** the response is HTTP 409 `{"error":"not_ready","status":<current>}`

### Requirement: Provider-abstracted image generation

Image generation SHALL be performed behind an `aiimage.Provider` interface so
the wire contract is independent of any single vendor. The first implementation
MUST be a fal adapter. The provider, model, and credentials MUST come from
backend configuration and MUST NOT be present in frontend code. The provider
MUST support both text-to-image generation and, when a `reference_image` is
supplied, image-to-image editing. The same provider instance MUST be reusable
by the chat agent's `generate_image` and `edit_image` tools (see
`backend-ai-chat`). The async submit→status→result contract MUST hold
regardless of whether the upstream provider is itself asynchronous (e.g. fal) or
synchronous (e.g. Gemini): for a synchronous upstream the backend runs the work
and serves the cached result via the result endpoint.

#### Scenario: Synchronous upstream still serves async contract

- **WHEN** the configured provider performs generation synchronously
- **THEN** the submit endpoint still returns immediately with a `request_id`, and the result becomes available via the status/result endpoints once the work completes

#### Scenario: Reference image performs an edit

- **WHEN** a submit request includes a non-empty `reference_image` together with an edit prompt
- **THEN** the provider performs an image-to-image edit and the resulting image reflects the edit instruction

#### Scenario: Provider reused by chat tools

- **WHEN** the chat agent invokes its `generate_image` or `edit_image` tool
- **THEN** the call is served by the same configured `aiimage.Provider` as the `/api/ai/images` endpoints, with no separate provider credential

#### Scenario: No provider key in the frontend

- **WHEN** a developer greps the `src/` tree for the image provider API key
- **THEN** no image provider key literal is present in frontend source; the value exists only in backend configuration
