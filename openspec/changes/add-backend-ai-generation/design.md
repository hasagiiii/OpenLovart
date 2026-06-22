## Context

Generation features run today inside Next.js route handlers:

- `POST /api/generate-design` — calls an OpenAI-compatible chat endpoint
  (`opentk.ai/v1`) with a **hard-coded API key** in source.
- `POST /api/generate-image` — calls Gemini (`@google/genai`) for image bytes.
- `POST /api/generate-video` + `GET /api/video-status` — async video via a
  sora-2 gateway (submit + poll).

The Go backend (`backend/`) already runs Gin with a clean
`business/<domain>/{repo.go,handlers.go}` layout, JWT cookie auth, CSRF, and
per-route rate-limiting. It owns `projects`, `canvas_elements`, and
`user_credits`. There is no model/agent code in the backend yet.

The user wants chat + design + image orchestration moved to the backend, built
on `trpc-agent-go` used **as a library** (not a `trpc-go` server), with
frontend-facing contracts that mirror OpenAI (chat) and fal (image).

## Goals / Non-Goals

**Goals:**
- Backend owns all model orchestration; the frontend only calls HTTP.
- No provider key ever reaches the browser or frontend source.
- Chat contract is OpenAI-shaped (`messages[]`, `stream`, `choices[].delta`)
  so it maps 1:1 onto `trpc-agent-go`'s native event stream.
- Image contract is fal-shaped (async: submit → status → result) and
  provider-abstracted so the underlying image provider can change.
- Reuse the existing auth + CSRF + rate-limit middleware unchanged.
- Keep Gin as the HTTP edge; introduce zero `trpc-go` server surface.

**Non-Goals:**
- Migrating video generation (stays on the frontend this round).
- **Durable** persistence of chat history to PostgreSQL. Per-session memory IS
  in scope this round (see D9) but uses the `trpc-agent-go` in-memory session
  service; a GORM/Redis-backed session store is a follow-up swap behind the same
  interface.
- Multi-agent orchestration, RAG/Knowledge, and MCP (a single agent with a
  fixed built-in tool set this round).
- Client-side / human-in-the-loop tool approval — tools run autonomously
  server-side; the client observes tool activity but does not execute or
  approve tool calls.
- Streaming partial **images** (fal returns final URLs; only chat text streams,
  with structured tool-activity events for image/search results).

## Decisions

### D1: `trpc-agent-go` as an embedded library inside Gin

Construct `model → llmagent → runner` once at startup (in `cmd/server`),
inject the `*runner.Runner` into the chat handler via the existing `Deps`
struct. The handler calls `runner.Run(ctx, userID, sessionID, msg)` and bridges
the returned event channel to the HTTP response. `ctx` is `c.Request.Context()`
so client disconnects cancel the agent (the recommended cancellation path).

- **Why**: trpc-agent-go's `Runner`/`Agent` are plain Go objects; embedding
  avoids a second service, second port, and the whole trpc-go server/naming
  stack. Auth/CSRF/limits stay in one place.
- **Alternatives**: (a) standalone trpc-go agent service called over RPC —
  rejected: operational overhead, no current need for an independent callee;
  (b) keep generation in Next.js — rejected: the key-leak and per-provider
  sprawl are exactly what we're removing.

### D2: Chat contract mirrors OpenAI chat-completions

`POST /api/ai/chat/completions` accepts `{ model?, messages:[{role,content}],
stream?, ... }` and returns either a single `chat.completion` JSON object or,
when `stream:true`, an SSE stream of `chat.completion.chunk` events terminated
by `data: [DONE]`.

- **Why**: trpc-agent-go events already carry
  `event.Object == "chat.completion.chunk"` and
  `event.Response.Choices[0].Delta.Content`. The handler can forward these with
  near-zero translation, and the frontend can consume them with standard
  OpenAI-style SSE parsing.
- The agent is **tool-using** (see D7): the same endpoint also routes intent to
  `generate_image` / `edit_image` / `web_search`. The contract is unchanged for
  callers; tool activity rides alongside the text stream as structured events.
- The "design assistant" behavior from the old `generate-design` becomes the
  agent's **system instruction**, not a separate endpoint. The frontend may
  still send a thin `POST /api/ai/design` convenience wrapper, but it is just
  the chat endpoint with a preset system prompt — kept optional.
- **Trade-off**: We expose an OpenAI-ish surface but it is OUR contract, not a
  drop-in OpenAI proxy. We will only implement the fields we use
  (`messages`, `stream`, `model`, `user` is injected server-side from the
  session — clients cannot spoof it). Two additive fields extend it: optional
  `session_id` (per-session memory, see D9) and optional `project_id` (canvas
  persistence target, see D7).

### D3: Image contract mirrors fal's async queue

Three endpoints under `/api/ai/images`:
- `POST /api/ai/images` → submit: returns `{ request_id, status }` with status
  `IN_QUEUE`.
- `GET /api/ai/images/:id/status` → `{ status: IN_QUEUE|IN_PROGRESS|COMPLETED|
  FAILED, ... }`.
- `GET /api/ai/images/:id` → final result `{ images:[{url,width,height,...}] }`.

Behind these sits an `aiimage.Provider` interface. The first implementation is
a **fal** adapter (chosen as the first provider); whether the upstream is itself
async (fal) or synchronous (e.g. Gemini) is hidden — for a synchronous upstream
the backend records the job, runs it, and serves the cached result on the
result endpoint.

The provider also covers image-to-image **editing**: a non-empty
`reference_image` plus an edit prompt routes to the provider's edit path. The
same provider instance is reused by the chat agent's `generate_image` and
`edit_image` tools (D7), so the tool path and the direct `/api/ai/images` path
are two entries into one provider.

- **Why**: matches the polling shape the frontend already uses for video, gives
  a uniform async contract, and decouples the wire format from any one vendor.
  fal-shaped status values are adopted verbatim so a future fal provider is a
  thin adapter.
- **Alternatives**: synchronous single POST returning bytes — rejected: long
  image latencies block the request, no progress/cancel story, and it would
  diverge from the video flow we want to fold in later.

### D4: Job store starts in-memory, interface-first

Image jobs are tracked via an `aiimage.Store` interface (Create, Get, Update).
Initial impl is an in-process map with TTL eviction; a GORM-backed impl is a
later swap (mirrors the `Limiter` interface pattern already in the codebase).

- **Why**: avoids a schema migration on day one while keeping the door open.
- **Risk**: in-memory jobs are lost on restart and not shared across replicas —
  acceptable for the current single-instance dev/self-host target; flagged for
  the durable follow-up.

### D5: Configuration & secrets

New env vars loaded by `config` (viper), with `trpc-agent-go`'s OpenAI model
reading `OPENAI_API_KEY` / `OPENAI_BASE_URL` from the process env:
- `AI_CHAT_MODEL` (default `gpt-5.4-mini`), `OPENAI_API_KEY`,
  `OPENAI_BASE_URL` (default `https://opentk.ai/v1` — the current self-host
  values).
- `AI_IMAGE_PROVIDER` (default `fal`), `AI_IMAGE_MODEL`, and the provider key
  (`AI_IMAGE_API_KEY` / `FAL_KEY`).
- `AI_SEARCH_PROVIDER` (default `brave`), `AI_SEARCH_API_KEY` for the
  `web_search` tool. The provider defaults to Brave; if no search key is
  configured the tool is not registered.
The hard-coded key in `generate-design/route.ts` is deleted with the route.

### D6: Routing & frontend wiring

Mount `/api/ai/*` inside the existing authed+CSRF `api` group in
`httpserver/router.go` (so generation inherits `RequireAuth` + `CSRF`). The
chat SSE response sets `Content-Type: text/event-stream` and disables CSRF only
if needed (it's a POST under the access cookie, so CSRF applies — the frontend
already attaches `X-CSRF-Token` via `fetchWithCsrf`). `next.config.ts` adds a
`/api/ai/:path*` rewrite to the backend; the frontend components switch their
fetch targets and the old Next routes are deleted.

### D7: Agentic intent routing via server-side tools

The `LLMAgent` is registered with three function tools and decides, per turn,
which (if any) to call based on the user's intent:
- `generate_image(prompt, size?, n?)` — text-to-image.
- `edit_image(prompt, reference_image, ...)` — image-to-image edit.
- `web_search(query, ...)` — live web lookup, results fed back to the model.

`generate_image` and `edit_image` delegate to the shared `aiimage` service
(same fal provider as the standalone image endpoints). Because a tool call must
return a usable result within the agent turn, the tool **blocks until the job
completes** (bounded by a per-tool timeout) and returns the resulting image
URL(s) to the model, which then references them in its reply.

**Canvas persistence**: when the chat request carries a `project_id`, images
produced by `generate_image` / `edit_image` are auto-persisted as
`canvas_elements` rows owned by that project (reusing the existing canvas
store), and the created element id(s) ride along in the `tool.result` event so
the frontend can place them on the canvas without a second round-trip. When no
`project_id` is supplied, the image URLs are still returned but nothing is
persisted.

- **Why**: the product need is "chat that does things", not just text. Putting
  routing in the model (function-calling) avoids brittle frontend intent
  heuristics and keeps a single conversational entry point; it also lets the
  model chain steps (e.g. `web_search` → then `generate_image`).
- **Tool activity on the wire**: during streaming the handler forwards the
  normal `chat.completion.chunk` text deltas **and** emits structured
  tool-activity events (`object: "tool.call"` with name+args; `object:
  "tool.result"` with produced image URLs / search hits) so the UI can show
  progress and the canvas can pick up generated images. Non-streaming responses
  include a tool-calls/tool-results summary alongside the final message.
- **Alternatives**: frontend keyword/intent detection then calling separate
  endpoints — rejected: duplicates intent logic, is fragile, and loses the
  model's ability to chain tools.
- **Loop/cost guard**: cap tool-call iterations per turn and apply the request
  context timeout so a tool storm cannot run unbounded.

### D8: Web search provider behind an interface

`web_search` is backed by a `websearch.Provider` interface
(`Search(ctx, query) → results`). The default provider is **Brave** (Brave
Search API); the provider and credential come from backend config
(`AI_SEARCH_PROVIDER` defaulting to `brave`, `AI_SEARCH_API_KEY`). No search key
reaches the frontend.

- **Why Brave**: a single, low-friction key, generous free tier, and clean JSON
  results; the interface keeps it swappable (Tavily / Serper / etc.) and
  consistent with the provider-abstraction approach used for images.
- **Degradation**: if no search key is configured, the `web_search` tool is not
  registered and the agent answers from model knowledge instead.

### D9: Per-session conversation memory

The `Runner` is wired with a `trpc-agent-go` **session service** so each
conversation's turns are retained server-side and replayed into the model on
the next turn. Sessions are keyed by `(appName, userID, sessionID)`:
- `userID` is the authenticated session user (never client-supplied).
- `sessionID` comes from an optional `session_id` field on the chat request; if
  absent the backend mints one and returns it (response field for non-stream, an
  early SSE event for stream) so the client can continue the same conversation.

The handler appends the latest user message to the session and lets the runner
recall prior turns; the OpenAI-shaped `messages[]` still validates the request
shape, but the **session store is the source of truth** for history, so clients
need only send the newest user message plus the `session_id`.

- **Why**: "remember each session's history" is a product requirement; the
  session service is the native trpc-agent-go mechanism and keeps memory logic
  out of the handler.
- **Implementation**: in-memory session service first (interface-first, like the
  image `Store`); a durable (GORM/Redis) session store is a later swap. Bounded
  so a single session cannot grow without limit (trim/oldest-eviction policy).
- **Alternatives**: stateless — client resends full `messages[]` each turn —
  rejected: pushes history management to every client and loses a single
  server-side memory the tools/agent can rely on.

## Risks / Trade-offs

- **SSE through CSRF + rewrite** → Streaming POSTs must carry the CSRF header;
  verify Next dev rewrite does not buffer the `text/event-stream` body.
  Mitigation: set `X-Accel-Buffering: no`, flush per chunk, integration-test
  the stream end-to-end through the rewrite.
- **trpc-agent-go transitive deps bloat `go.mod`** → Mitigation: `go get` and
  audit the dependency delta before committing; pin a known-good version.
- **In-memory image jobs lost on restart / not multi-replica** (D4) →
  Mitigation: documented limitation; `Store` interface enables a GORM swap.
- **Provider abstraction may leak vendor-specific options** (size, aspect,
  reference image) → Mitigation: keep a small typed core (`prompt`, `size`,
  `n`, optional `reference_image`) plus a passthrough `provider_options` map.
- **Tool calls block the agent turn** (D7) → a slow fal job stalls the chat
  reply. Mitigation: per-tool timeout + iteration cap; surface `tool.call`
  progress events so the UI stays responsive; the direct async image endpoints
  remain for fire-and-forget generation.
- **Tool-activity events extend the OpenAI-ish surface** (D7) → `tool.call` /
  `tool.result` are our additions, not standard OpenAI chunks. Mitigation: keep
  text deltas strictly OpenAI-shaped so a vanilla SSE consumer still works;
  tool events are additive and ignorable.
- **In-memory sessions lost on restart / not multi-replica** (D9) → same
  trade-off as the image job store; acceptable for single-instance self-host,
  flagged for the durable session-store follow-up. Unbounded session growth is
  mitigated by a per-session trim policy.
- **Canvas auto-persist needs a `project_id`** (D7) → without it the backend
  cannot attribute the element; mitigation: persist only when `project_id` is
  present and owned by the session user, otherwise just return URLs.
- **Credits/quota not yet enforced on generation** → out of scope here but the
  authed handler is the natural future enforcement point; noted, not built.

## Migration Plan

1. Add backend deps + config; build `internal/agent` (runner) and
   `internal/business/{aichat,aiimage}` with handlers; wire routes.
2. Add `/api/ai/:path*` rewrite in `next.config.ts`.
3. Repoint `DesignChat` (chat/SSE) and `ImageGeneratorPanel`/`Dialog` to the
   new endpoints.
4. Delete `src/app/api/generate-design` and `src/app/api/generate-image`
   (and their hard-coded/leaked keys).
5. Rollback: revert frontend component target + rewrite, restore the deleted
   Next routes (kept in git history); backend additions are additive and inert
   if unused.

## Open Questions

Resolved with the change owner:
- **Chat model / base URL**: use the current self-host values
  (`gpt-5.4-mini` @ `https://opentk.ai/v1`) as the documented defaults.
- **Image provider order**: ship the **fal** adapter first (text-to-image +
  image-to-image edit); a Gemini adapter can follow as a drop-in `Provider`.
- **Web search default**: **Brave** (`AI_SEARCH_PROVIDER=brave`).
- **Chat-generated images**: auto-persist as `canvas_elements` when a
  `project_id` is supplied (D7), returning the created element id(s) to the
  client.
- **Session memory**: keep per-session history server-side via the
  `trpc-agent-go` session service (D9), in-memory first.

Still open:
- Durable session/history store backend (GORM vs Redis) and the per-session
  trim/retention policy — deferred to the durability follow-up.
