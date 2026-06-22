## ADDED Requirements

### Requirement: OpenAI-shaped chat completion endpoint

The system SHALL expose `POST /api/ai/chat/completions` under the
authenticated, CSRF-protected business route group. The request body MUST
accept an OpenAI-shaped payload: `messages` (a non-empty array of
`{role, content}` where `role` is one of `system`, `user`, `assistant`), an
optional `model` string, an optional `stream` boolean (default `false`), an
optional `session_id` string (per-session memory, see "Per-session conversation
memory"), an optional `project_id` string (canvas persistence target, see
"Generated images persisted to the canvas"), and MAY accept additional
generation options. The handler MUST reject a missing or empty `messages` array
with HTTP 400 `{"error":"invalid_request"}`. The end user is identified
server-side from the authenticated session; any client-supplied `user` field
MUST be ignored.

#### Scenario: Non-streaming completion

- **WHEN** an authenticated client POSTs `{"messages":[{"role":"user","content":"hi"}]}` with `stream` unset or `false`
- **THEN** the backend runs the agent to completion and responds HTTP 200 with a JSON object whose `object` is `chat.completion` and whose `choices[0].message.content` holds the assistant's full reply

#### Scenario: Missing messages rejected

- **WHEN** an authenticated client POSTs a body with no `messages` or an empty `messages` array
- **THEN** the backend responds HTTP 400 with body `{"error":"invalid_request"}` and does not invoke the model

#### Scenario: Unauthenticated request rejected

- **WHEN** a client without a valid session cookie POSTs to `/api/ai/chat/completions`
- **THEN** the backend responds HTTP 401 `{"error":"unauthenticated"}` before any model call

#### Scenario: Client-supplied user id is ignored

- **WHEN** an authenticated client includes a `user` field in the request body
- **THEN** the backend uses the session user id for attribution and disregards the supplied value

### Requirement: Server-Sent Events streaming

When the request sets `stream:true`, the system SHALL respond with
`Content-Type: text/event-stream` and emit each model delta as an SSE `data:`
line whose payload is a JSON object with `object` equal to
`chat.completion.chunk` and the incremental text in `choices[0].delta.content`.
The stream MUST be terminated by a final `data: [DONE]` line. The handler MUST
flush each chunk as it is produced and MUST NOT buffer the full response.

#### Scenario: Streamed deltas then DONE

- **WHEN** an authenticated client POSTs `{"messages":[...],"stream":true}`
- **THEN** the response is `text/event-stream`, the client receives one or more `chat.completion.chunk` events whose concatenated `choices[0].delta.content` reconstruct the full reply, followed by a terminal `data: [DONE]` line

#### Scenario: Client disconnect cancels the run

- **WHEN** the client closes the connection mid-stream
- **THEN** the backend cancels the request context, the agent run stops issuing further model/tool calls, and server resources for that run are released

### Requirement: Agent backed by trpc-agent-go

The chat endpoint SHALL be backed by a `trpc-agent-go` `Runner` driving an
`LLMAgent` over an OpenAI-compatible model. The runner/agent/model MUST be
constructed once during startup and reused across requests, with the
intent-routing tools (see "Intent-routed tool calling") registered on the agent
at construction time and a session service (see "Per-session conversation
memory") wired into the runner. The agent MUST be
configured with a design-assistant system instruction so that a bare user
prompt yields design-oriented suggestions equivalent to the former
`/api/generate-design` behavior. Provider credentials and model selection MUST
come from backend configuration and MUST NOT be present in frontend code.

#### Scenario: Reused runner across requests

- **WHEN** two separate authenticated chat requests arrive
- **THEN** both are served by the same long-lived runner/agent instance without re-initializing the model client per request

#### Scenario: Design-assistant default behavior

- **WHEN** an authenticated client sends a single user message describing something to design (no system message of its own)
- **THEN** the assistant reply reflects the design-assistant system instruction (layout, colors, typography, visual elements guidance)

#### Scenario: No provider key in the frontend

- **WHEN** a developer greps the `src/` tree for the chat provider API key
- **THEN** no provider key or provider base URL literal is present in frontend source; these values exist only in backend configuration

### Requirement: Intent-routed tool calling

The chat agent SHALL be registered with built-in tools and decide per turn,
from the user's intent, whether to call them: `generate_image` (text-to-image),
`edit_image` (image-to-image edit given a reference image), and `web_search`
(live web lookup, backed by Brave by default). Tools MUST run server-side; the client neither executes nor
approves them. `generate_image` and `edit_image` MUST delegate to the same
backend image provider used by the `/api/ai/images` endpoints, blocking until
the image is produced and returning its URL(s) to the model. Each tool
invocation MUST be bounded by a timeout and the number of tool iterations per
turn MUST be capped. When no web search provider is configured, the
`web_search` tool MUST NOT be offered and the agent MUST answer from model
knowledge instead.

#### Scenario: Image request triggers generate_image

- **WHEN** an authenticated user sends "draw me a poster of a red sports car"
- **THEN** the agent calls the `generate_image` tool, the backend produces the image via the configured provider, and the assistant reply references the resulting image URL(s)

#### Scenario: Edit request triggers edit_image

- **WHEN** the user supplies a reference image and asks to "make the sky purple"
- **THEN** the agent calls the `edit_image` tool with that reference image and the edited image URL is returned in the reply

#### Scenario: Freshness request triggers web_search

- **WHEN** the user asks something requiring current information and a search provider is configured
- **THEN** the agent calls the `web_search` tool and grounds its answer on the returned results

#### Scenario: Plain conversation needs no tool

- **WHEN** the user sends a message that needs no generation or lookup (e.g. "suggest a color palette for a calm landing page")
- **THEN** the agent replies directly without invoking any tool

#### Scenario: Tool disabled when unconfigured

- **WHEN** no web search provider credential is configured and the user asks a search-style question
- **THEN** the `web_search` tool is not registered, no search call is attempted, and the agent answers from its own knowledge

### Requirement: Tool activity surfaced to the client

The chat response SHALL expose tool activity to the client in addition to the
assistant text. In streaming mode the system MUST, alongside the
`chat.completion.chunk` text deltas, emit structured tool-activity SSE events: a
`tool.call` event carrying the tool name and arguments when a tool starts, and a
`tool.result` event carrying the tool output (image URLs for image tools, result
hits for `web_search`) when it finishes. In non-streaming mode the final JSON
MUST include the tool calls made and their results. A tool failure MUST be
reported as a `tool.result` event (or non-stream field) with an `error` rather
than aborting the whole turn when the agent can still respond.

#### Scenario: Streamed tool activity for image generation

- **WHEN** a streaming chat request causes the agent to generate an image
- **THEN** the client receives a `tool.call` event for `generate_image`, then a `tool.result` event containing the image URL(s), interleaved with the text deltas, before the terminal `data: [DONE]` line

#### Scenario: Non-streaming includes tool results

- **WHEN** a non-streaming chat request causes one or more tool calls
- **THEN** the HTTP 200 JSON body includes the tool calls and their results (e.g. generated image URLs) together with the assistant message

#### Scenario: Tool failure does not abort the turn

- **WHEN** a tool call fails (e.g. the image provider errors) but the agent can still produce a textual reply
- **THEN** a `tool.result` with an `error` is surfaced and the assistant still returns a coherent message instead of an HTTP 5xx

### Requirement: Per-session conversation memory

The chat agent SHALL retain conversation history per session server-side using a
`trpc-agent-go` session service so that earlier turns inform later replies within
the same session. Sessions MUST be keyed by the authenticated user id together
with a session id; the user id MUST come from the session and MUST NOT be
client-supplied. The request MAY carry a `session_id`; when absent the backend
MUST mint one and return it to the client (a response field for non-streaming, an
early event for streaming) so the conversation can be continued. The server-side
session store MUST be the source of truth for history, so a client MAY send only
the newest user message together with the `session_id` and still receive a reply
grounded in prior turns. History storage MAY be in-memory for this change and
MUST be bounded so a single session cannot grow without limit.

#### Scenario: New session id minted and returned

- **WHEN** an authenticated client sends a chat request without a `session_id`
- **THEN** the backend creates a session, serves the reply, and returns the generated `session_id` to the client (a JSON field for non-streaming, an early SSE event for streaming)

#### Scenario: Prior turns remembered within a session

- **WHEN** the client sends a follow-up message carrying the same `session_id` (e.g. "make it blue" after an earlier turn established a subject)
- **THEN** the agent's reply reflects the earlier turns of that session without the client resending the full history

#### Scenario: Sessions isolated per user

- **WHEN** two different authenticated users happen to use the same `session_id` value
- **THEN** their histories remain separate because sessions are keyed by user id plus session id

### Requirement: Generated images persisted to the canvas

The system SHALL persist tool-generated images as `canvas_elements` when a chat
request carries a `project_id`. The created rows MUST be owned by that project
(reusing the existing canvas store) and the created canvas element id(s) MUST be
included in the corresponding `tool.result` (and in the non-streaming
tool-results summary). The `project_id` MUST be validated as owned
by the authenticated session user; an unknown or non-owned `project_id` MUST NOT
cause element creation. When no `project_id` is supplied, the image URL(s) MUST
still be returned but no canvas element is persisted.

#### Scenario: Generated image persisted when project_id present

- **WHEN** an authenticated user sends a chat request with a `project_id` they own and the agent calls `generate_image`
- **THEN** a `canvas_elements` row is created under that project and the `tool.result` event includes both the image URL(s) and the created canvas element id(s)

#### Scenario: No project_id means no persistence

- **WHEN** the agent generates an image but the request carried no `project_id`
- **THEN** the image URL(s) are returned in the `tool.result` but no canvas element is created

#### Scenario: Non-owned project_id does not persist

- **WHEN** a request supplies a `project_id` that does not belong to the session user
- **THEN** no canvas element is created for that project and the image URL(s) are still returned
