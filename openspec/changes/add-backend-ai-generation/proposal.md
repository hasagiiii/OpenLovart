## Why

The AI generation features (design chat, image generation) currently live in
Next.js route handlers under `src/app/api/*`. This is fragile and insecure:
provider API keys are read in (and in one case **hard-coded into**) frontend
code, every feature talks to a different provider with an ad-hoc payload, and
there is no shared auth/credits/quota path. We want a single backend that owns
all model orchestration via `trpc-agent-go` (running as a library inside the
existing Gin server) and exposes clean, provider-agnostic HTTP contracts to the
frontend.

## What Changes

- Introduce a backend "AI chat" capability: an OpenAI-shaped chat-completions
  endpoint that drives a `trpc-agent-go` `Runner` + `LLMAgent`. The agent is
  **tool-using**: it interprets user intent and autonomously decides whether to
  call `generate_image`, `edit_image`, or `web_search` tools (or to just reply
  in text). It is also **session-aware**: per-session conversation history is
  kept server-side via a `trpc-agent-go` session service so multi-turn context
  is remembered. Images produced by the image tools are auto-persisted as
  `canvas_elements` when the request carries a `project_id`. Replaces the
  frontend `/api/generate-design` route (design suggestions become a system
  prompt on the same agent).
- Introduce a backend "AI image" capability: a fal-shaped asynchronous
  generation **and editing** contract (submit → poll status → fetch result).
  Replaces the frontend `/api/generate-image` route. The same image provider is
  reused by the chat agent's `generate_image` / `edit_image` tools, and the
  async submit/status shape is designed to also absorb the existing video flow
  later.
- Add `trpc.group/trpc-go/trpc-agent-go` as a backend dependency, used as a
  library — Gin remains the HTTP edge (auth, cookies, CSRF, rate-limit all
  unchanged). No `trpc-go` server is introduced.
- Move all provider credentials and model selection to backend config
  (env-driven). **BREAKING**: the hard-coded API key in
  `src/app/api/generate-design/route.ts` is removed.
- **BREAKING**: Remove the frontend generation route handlers and repoint the
  frontend components (`DesignChat`, `ImageGeneratorPanel`/`Dialog`) to the new
  backend endpoints via the existing same-origin rewrite mechanism.

## Capabilities

### New Capabilities
- `backend-ai-chat`: Authenticated, OpenAI-shaped chat endpoint backed by a
  **tool-using, session-aware** `trpc-agent-go` agent (streaming via SSE +
  non-streaming JSON). The agent routes user intent to server-side tools —
  `generate_image` and `edit_image` (delegating to the image provider) and
  `web_search` (Brave by default) — keeps per-session conversation history via a
  session service, and persists tool-generated images into `canvas_elements`
  when a `project_id` is supplied. It owns request validation, session/user
  wiring, and the design-assistant system prompt. Tool activity is surfaced to
  the client alongside the text stream.
- `backend-ai-image`: Authenticated, fal-shaped asynchronous image generation
  and editing (submit job → query status → fetch result), provider-abstracted
  behind a backend image service that is also reused by the chat agent's
  `generate_image` / `edit_image` tools.

### Modified Capabilities
- `backend-runtime`: Config loads new model/image provider settings **and an
  optional web search provider for the chat agent's `web_search` tool**; the Gin
  router mounts the new `/api/ai/*` routes under the existing auth+CSRF group.
- `frontend-auth-and-data`: Frontend generation features call the backend over
  same-origin fetch (cookies + CSRF) instead of local Next.js route handlers;
  the local generation routes are removed.

## Impact

- **Backend (new)**: `internal/business/aichat` (agent + handlers),
  `internal/business/aiimage` (image service + handlers), `internal/agent`
  (trpc-agent-go runner/agent construction + tool registration + session
  service for per-session memory), `internal/websearch` (Brave-backed web search
  provider for the `web_search` tool), config additions, router wiring.
- **Backend (reused)**: the existing `canvas_elements` store is reused so the
  chat image tools can auto-persist generated images for a given `project_id`.
- **Backend deps**: `+ trpc.group/trpc-go/trpc-agent-go` (and its transitive
  deps) in `go.mod`.
- **Frontend (changed/removed)**: delete `src/app/api/generate-design`,
  `generate-image`; update `DesignChat.tsx`, `ImageGeneratorPanel.tsx`,
  `ImageGeneratorDialog.tsx`; add new rewrite entries in `next.config.ts`.
- **Config/secrets**: new env vars for LLM, image, and (optional) web search
  providers; removal of the leaked hard-coded key.
- **Out of scope**: video generation migration (`generate-video`,
  `video-status`) — kept on the frontend for now but the async image contract
  is shaped to absorb it in a follow-up.
