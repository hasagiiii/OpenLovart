## 1. Backend dependencies & configuration

- [x] 1.1 `go get trpc.group/trpc-go/trpc-agent-go@<pinned-version>` from `backend/`; audit the transitive dependency delta in `go.mod`/`go.sum` and commit a known-good pin
- [x] 1.2 Extend `internal/config` with AI settings: `AI_CHAT_MODEL` (default `gpt-5.4-mini`), `OPENAI_API_KEY`, `OPENAI_BASE_URL` (default `https://opentk.ai/v1`), `AI_IMAGE_PROVIDER` (default `fal`), `AI_IMAGE_MODEL`, image provider credential (`AI_IMAGE_API_KEY` / `FAL_KEY`), and `AI_SEARCH_PROVIDER` (default `brave`) / `AI_SEARCH_API_KEY`; document them in `backend/.env.example`
- [x] 1.3 Add config unit tests covering the new defaults (incl. `brave` search default), env precedence, and the "search key absent → tool disabled" path

## 2. Image service & provider (fal)

- [x] 2.1 Define `aiimage.Provider` interface with generate (text-to-image) and edit (image-to-image, given `reference_image`) methods; implement a **fal** adapter (submit → poll fal status → fetch result), keyed by config; map vendor result → `{images:[{url,width,height,content_type}]}`
- [x] 2.2 Define `aiimage.Store` interface (Create/Get/Update with owner + status) and an in-memory TTL implementation
- [x] 2.3 Implement the async worker: on submit, create job (`IN_QUEUE`), run generation/edit in a goroutine, transition `IN_PROGRESS`→`COMPLETED`/`FAILED`, cache the result
- [x] 2.4 Provide a synchronous `Generate`/`Edit` helper on the service (blocks until done, returns URLs) so the chat agent tools can reuse the same provider
- [x] 2.5 Tests: fal generate + edit happy paths, failure mapping, store TTL eviction

## 3. Web search provider (Brave)

- [x] 3.1 Define `websearch.Provider` interface (`Search(ctx, query) → results`) and implement a **Brave** adapter (Brave Search API) selected by `AI_SEARCH_PROVIDER` (default `brave`), keyed by `AI_SEARCH_API_KEY`
- [x] 3.2 Build a "disabled" no-op path when no search credential is configured (so the tool is simply not registered)
- [x] 3.3 Tests: Brave adapter maps results to hits; absent key yields the disabled state

## 4. Agent runtime & tools (trpc-agent-go as library)

- [x] 4.1 Create `internal/agent`: construct the OpenAI-compatible `model`, an `llmagent.LLMAgent` with the design-assistant system instruction, and a reusable `*runner.Runner`
- [x] 4.2 Register the three function tools on the agent: `generate_image` and `edit_image` (delegating to the `aiimage` service from §2) and `web_search` (delegating to §3); only register `web_search` when a search key is configured
- [x] 4.3 Apply per-tool timeouts and cap tool-call iterations per turn; bind tool execution to the request context
- [x] 4.4 Wire a `trpc-agent-go` session service into the runner (in-memory impl, interface-first) so per-session history is retained, keyed by `(userID, sessionID)`; apply a per-session bound/trim policy
- [x] 4.5 Expose a small typed `ChatService` facade: `Run(ctx, userID, sessionID, projectID, messages, stream)` — minting a `sessionID` when absent, returning it plus either the full completion (with tool-call/result summary) or the event channel
- [x] 4.6 Wire the runner/agent/tool/session construction into `cmd/server/main.go` startup and add it to `httpserver.Deps` (including the canvas store for tool-side persistence)

## 5. Chat endpoint (backend-ai-chat)

- [x] 5.1 Create `internal/business/aichat/handlers.go`: validate OpenAI-shaped body (`messages` required, ignore client `user`), accept optional `session_id` and `project_id`, 400 on invalid
- [x] 5.2 Resolve the session: use the supplied `session_id` or mint a new one; return it to the client (JSON field non-stream, early SSE event for stream)
- [x] 5.3 Implement non-streaming path → `chat.completion` JSON with `choices[0].message.content`, the resolved `session_id`, plus the tool calls made and their results
- [x] 5.4 Implement streaming path → `text/event-stream`, forward `chat.completion.chunk` text deltas with per-chunk flush + `X-Accel-Buffering: no`, terminate with `data: [DONE]`
- [x] 5.5 Emit structured tool-activity events alongside the text stream: `tool.call` (name + args) on tool start, `tool.result` (image URLs + created canvas element id(s) / search hits, or `error`) on finish; include the same summary in the non-streaming body
- [x] 5.6 When `project_id` is present and owned by the session user, persist tool-generated images as `canvas_elements` (reuse the existing canvas store) and attach the created element id(s) to the `tool.result`; skip persistence when absent or non-owned
- [x] 5.7 Bind request context so client disconnect cancels the run (and in-flight tool calls); drain the event channel on cancel
- [x] 5.8 Mount `POST /api/ai/chat/completions` in the authed+CSRF `api` group in `router.go`
- [x] 5.9 Tests: 401 unauth, 400 missing messages, non-stream content, stream deltas+DONE, disconnect cancellation, intent routing to each tool, plain reply with no tool, `web_search` disabled when unconfigured, tool-failure does not abort the turn, session_id minted+returned, follow-up turn remembers prior context, canvas persistence with/without `project_id` and non-owned `project_id`

## 6. Image endpoints (backend-ai-image)

- [x] 6.1 `POST /api/ai/images` handler: validate `prompt`, route to generate or edit when `reference_image` is present, 202 `{request_id,status:IN_QUEUE}`, attach session-user ownership
- [x] 6.2 `GET /api/ai/images/:id/status` handler: owner-scoped status (`IN_QUEUE|IN_PROGRESS|COMPLETED|FAILED`, `error` on failure); 404 for non-owner/unknown
- [x] 6.3 `GET /api/ai/images/:id` handler: completed result, 409 `not_ready` otherwise, owner-scoped 404
- [x] 6.4 Mount the three image routes in the authed+CSRF `api` group in `router.go`
- [x] 6.5 Tests: submit→poll→result happy path, edit path, missing prompt 400, non-owner 404, result-before-complete 409, failure path

## 7. Frontend rewiring

- [x] 7.1 Add `{ source: '/api/ai/:path*', destination: '${BACKEND_DEV_URL}/api/ai/:path*' }` to `next.config.ts` rewrites
- [x] 7.2 Update `DesignChat.tsx` to call `POST /api/ai/chat/completions` via `fetchWithCsrf`, sending the latest `messages[]` plus the current `project_id` and the stored `session_id` (persisting the `session_id` returned by the backend for follow-up turns); consume the SSE stream — render `choices[0].delta.content` text and handle `tool.call`/`tool.result` events (show generation progress, render produced image URLs, and place returned canvas element id(s) on the canvas)
- [x] 7.3 Update `ImageGeneratorPanel.tsx` and `ImageGeneratorDialog.tsx` to submit `/api/ai/images`, poll status, fetch result, render `images[].url`
- [x] 7.4 Delete `src/app/api/generate-design/route.ts` and `src/app/api/generate-image/route.ts` (removing the hard-coded/leaked keys)
- [x] 7.5 Grep `src/` to confirm no chat/image/search provider key or base URL literal remains

## 8. Verification

- [x] 8.1 `go build ./...` and `go test ./...` pass in `backend/`
- [x] 8.2 `npm run lint` / `tsc --noEmit` pass for the frontend
- [ ] 8.3 Manual end-to-end through `npm run dev`: a "design me X" prompt streams text, an image-intent prompt triggers `generate_image` and renders + persists the result on the canvas (with `project_id`), an edit prompt with a reference image triggers `edit_image`, a follow-up prompt reuses the returned `session_id` and shows remembered context, and a freshness prompt triggers `web_search` (Brave) — all through the same-origin rewrite with cookie + CSRF
- [x] 8.4 Confirm `/api/generate-video` and `/api/video-status` still work unchanged on Next
