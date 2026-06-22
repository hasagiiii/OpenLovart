// Frontend API client for the Go backend.
//
// All state-changing calls flow through fetchWithCsrf which:
//   1. mirrors the `csrf` cookie into the `X-CSRF-Token` header
//      (double-submit pattern; the cookie is intentionally readable via JS)
//   2. transparently retries once on 401 by POSTing /api/auth/refresh
//
// We never store any signing secret, JWT, or refresh token in JS-accessible
// state — the backend manages all of that via HttpOnly cookies.

// ---------- Types (mirrors backend DTOs) ----------

export interface Project {
  id: string;
  user_id: string;
  title: string;
  thumbnail: string | null;
  created_at: string;
  updated_at: string;
}

export interface ProjectListResponse {
  items: Project[];
  nextCursor: string | null;
}

export interface CanvasElement {
  id: string;
  project_id: string;
  // Backend stores arbitrary JSON; we type it as `unknown` to force callers
  // to validate at the edge (lovart canvas page already does its own narrowing).
  element_data: unknown;
  created_at: string;
  updated_at: string;
}

export interface CanvasElementInput {
  // Optional: when supplied, the backend preserves the id; otherwise a new
  // uuid is minted. element_data is required.
  id?: string;
  element_data: unknown;
}

export interface Credits {
  user_id: string;
  credits: number;
  created_at: string;
  updated_at: string;
}

export interface ApiErrorBody {
  error?: string;
  message?: string;
}

export class ApiError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

// ---------- CSRF + refresh-then-retry plumbing ----------

function readCookie(name: string): string | null {
  if (typeof document === 'undefined') return null;
  const target = `${name}=`;
  for (const part of document.cookie.split(';')) {
    const trimmed = part.trim();
    if (trimmed.startsWith(target)) {
      return decodeURIComponent(trimmed.slice(target.length));
    }
  }
  return null;
}

const SAFE_METHODS = new Set(['GET', 'HEAD', 'OPTIONS']);

async function tryRefresh(): Promise<boolean> {
  try {
    const res = await fetch('/api/auth/refresh', {
      method: 'POST',
      credentials: 'include',
    });
    return res.ok;
  } catch {
    return false;
  }
}

interface InternalOpts extends RequestInit {
  // Skip the refresh-then-retry loop (used by the refresh call itself).
  skipRefresh?: boolean;
}

export async function fetchWithCsrf(
  input: string,
  init: InternalOpts = {},
): Promise<Response> {
  const method = (init.method ?? 'GET').toUpperCase();
  const isUnsafe = !SAFE_METHODS.has(method);

  const buildHeaders = (): Headers => {
    const h = new Headers(init.headers ?? {});
    if (isUnsafe && !h.has('X-CSRF-Token')) {
      const token = readCookie('csrf');
      if (token) h.set('X-CSRF-Token', token);
    }
    if (
      isUnsafe &&
      init.body &&
      typeof init.body === 'string' &&
      !h.has('Content-Type')
    ) {
      h.set('Content-Type', 'application/json');
    }
    return h;
  };

  // For state-changing calls, ensure we have a CSRF cookie before firing.
  // If the cookie is missing, refresh once (which mints a new csrf cookie)
  // and proceed regardless of the retry outcome — the server will respond
  // with 403 if it's truly missing.
  if (isUnsafe && !readCookie('csrf') && !init.skipRefresh) {
    await tryRefresh();
  }

  const res = await fetch(input, {
    ...init,
    headers: buildHeaders(),
    credentials: 'include',
  });

  if (res.status === 401 && !init.skipRefresh) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      // Retry once with fresh cookies. Re-read the csrf cookie before retry.
      return fetch(input, {
        ...init,
        headers: buildHeaders(),
        credentials: 'include',
      });
    }
  }

  return res;
}

// ---------- Helpers ----------

async function jsonOrThrow<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let body: ApiErrorBody = {};
    try {
      body = (await res.json()) as ApiErrorBody;
    } catch {
      // ignore — fall back to status text
    }
    throw new ApiError(
      body.error ?? 'request_failed',
      body.message ?? res.statusText,
      res.status,
    );
  }
  return (await res.json()) as T;
}

// ---------- Projects ----------

export async function listProjects(opts: {
  limit?: number;
  cursor?: string | null;
} = {}): Promise<ProjectListResponse> {
  const params = new URLSearchParams();
  if (opts.limit) params.set('limit', String(opts.limit));
  if (opts.cursor) params.set('cursor', opts.cursor);
  const qs = params.toString();
  const url = `/api/projects${qs ? `?${qs}` : ''}`;
  const res = await fetchWithCsrf(url);
  return jsonOrThrow<ProjectListResponse>(res);
}

export async function getProject(id: string): Promise<Project> {
  const res = await fetchWithCsrf(`/api/projects/${encodeURIComponent(id)}`);
  return jsonOrThrow<Project>(res);
}

export async function createProject(input: {
  title: string;
  thumbnail?: string | null;
}): Promise<Project> {
  const res = await fetchWithCsrf('/api/projects', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  return jsonOrThrow<Project>(res);
}

export async function updateProject(
  id: string,
  patch: { title?: string; thumbnail?: string | null },
): Promise<Project> {
  const res = await fetchWithCsrf(`/api/projects/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(patch),
  });
  return jsonOrThrow<Project>(res);
}

export async function deleteProject(id: string): Promise<void> {
  const res = await fetchWithCsrf(`/api/projects/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
  await jsonOrThrow<{ ok: boolean }>(res);
}

// ---------- Canvas elements ----------

export async function getCanvasElements(
  projectId: string,
): Promise<CanvasElement[]> {
  const res = await fetchWithCsrf(
    `/api/projects/${encodeURIComponent(projectId)}/canvas-elements`,
  );
  const body = await jsonOrThrow<{ items: CanvasElement[] }>(res);
  return body.items;
}

export async function putCanvasElements(
  projectId: string,
  elements: CanvasElementInput[],
): Promise<CanvasElement[]> {
  const res = await fetchWithCsrf(
    `/api/projects/${encodeURIComponent(projectId)}/canvas-elements`,
    {
      method: 'PUT',
      body: JSON.stringify({ elements }),
    },
  );
  const body = await jsonOrThrow<{ items: CanvasElement[] }>(res);
  return body.items;
}

// ---------- Credits ----------

export async function getCredits(): Promise<Credits> {
  const res = await fetchWithCsrf('/api/credits');
  return jsonOrThrow<Credits>(res);
}

// ---------- AI: chat ----------

export interface ChatMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
}

export interface ToolActivity {
  object: 'tool.call' | 'tool.result';
  name: string;
  arguments?: unknown;
  // For image tools this is `{ images: [{ url, canvas_element_id, ... }] }`;
  // for web_search it is `{ results: [...] }`. Typed as unknown — callers
  // narrow at the edge.
  result?: unknown;
  error?: string;
}

export interface ChatCompletion {
  id: string;
  object: string;
  session_id: string;
  model: string;
  choices: Array<{
    index: number;
    message: { role: string; content: string };
    finish_reason?: string;
  }>;
  tool_activity?: ToolActivity[];
}

// chatCompletion performs a non-streaming chat turn. The backend session store
// owns history, so callers send only the latest user message plus the resolved
// session_id (echoed back for follow-up turns) and the current project_id.
export async function chatCompletion(input: {
  messages: ChatMessage[];
  sessionId?: string | null;
  projectId?: string | null;
}): Promise<ChatCompletion> {
  const res = await fetchWithCsrf('/api/ai/chat/completions', {
    method: 'POST',
    body: JSON.stringify({
      messages: input.messages,
      stream: false,
      session_id: input.sessionId ?? undefined,
      project_id: input.projectId ?? undefined,
    }),
  });
  return jsonOrThrow<ChatCompletion>(res);
}

// SSE event shapes emitted by the streaming chat endpoint.
export type ChatStreamEvent =
  | { object: 'session'; session_id: string }
  | {
      object: 'chat.completion.chunk';
      session_id?: string;
      model?: string;
      choices: Array<{ index: number; delta: { content?: string } }>;
    }
  | { object: 'tool.call'; name: string; arguments?: unknown }
  | { object: 'tool.result'; name: string; result?: unknown; error?: string }
  | { object: 'error'; error: string };

// chatStream performs a streaming chat turn, invoking onEvent for each parsed
// SSE frame until the `[DONE]` terminator. Honors AbortSignal for cancellation.
export async function chatStream(
  input: {
    messages: ChatMessage[];
    sessionId?: string | null;
    projectId?: string | null;
  },
  onEvent: (ev: ChatStreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetchWithCsrf('/api/ai/chat/completions', {
    method: 'POST',
    body: JSON.stringify({
      messages: input.messages,
      stream: true,
      session_id: input.sessionId ?? undefined,
      project_id: input.projectId ?? undefined,
    }),
    signal,
  });

  if (!res.ok || !res.body) {
    let body: ApiErrorBody = {};
    try {
      body = (await res.json()) as ApiErrorBody;
    } catch {
      // ignore
    }
    throw new ApiError(
      body.error ?? 'request_failed',
      body.message ?? res.statusText,
      res.status,
    );
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';

  const dispatch = (raw: string) => {
    // Each SSE frame is one or more `data: ...` lines.
    for (const line of raw.split('\n')) {
      const trimmed = line.trim();
      if (!trimmed.startsWith('data:')) continue;
      const payload = trimmed.slice('data:'.length).trim();
      if (!payload || payload === '[DONE]') continue;
      try {
        onEvent(JSON.parse(payload) as ChatStreamEvent);
      } catch {
        // ignore malformed frame
      }
    }
  };

  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    // Frames are separated by a blank line.
    let sep: number;
    while ((sep = buffer.indexOf('\n\n')) !== -1) {
      const frame = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      dispatch(frame);
    }
  }
  if (buffer.trim()) dispatch(buffer);
}

// ---------- AI: image generation ----------

export interface AiImage {
  url: string;
  width: number;
  height: number;
  content_type: string;
}

export type ImageJobStatus =
  | 'IN_QUEUE'
  | 'IN_PROGRESS'
  | 'COMPLETED'
  | 'FAILED';

export interface ImageSubmitInput {
  prompt: string;
  size?: string;
  n?: number;
  reference_image?: string;
  provider_options?: Record<string, unknown>;
}

export async function submitImage(
  input: ImageSubmitInput,
): Promise<{ request_id: string; status: ImageJobStatus }> {
  const res = await fetchWithCsrf('/api/ai/images', {
    method: 'POST',
    body: JSON.stringify(input),
  });
  return jsonOrThrow<{ request_id: string; status: ImageJobStatus }>(res);
}

export async function getImageStatus(
  id: string,
): Promise<{ request_id: string; status: ImageJobStatus; error?: string }> {
  const res = await fetchWithCsrf(`/api/ai/images/${encodeURIComponent(id)}/status`);
  return jsonOrThrow<{
    request_id: string;
    status: ImageJobStatus;
    error?: string;
  }>(res);
}

export async function getImageResult(id: string): Promise<{ images: AiImage[] }> {
  const res = await fetchWithCsrf(`/api/ai/images/${encodeURIComponent(id)}`);
  return jsonOrThrow<{ images: AiImage[] }>(res);
}

// generateImage submits a job and polls until it completes (or fails),
// returning the produced images. Polls every `intervalMs` up to `timeoutMs`.
export async function generateImage(
  input: ImageSubmitInput,
  opts: { intervalMs?: number; timeoutMs?: number; signal?: AbortSignal } = {},
): Promise<AiImage[]> {
  const intervalMs = opts.intervalMs ?? 1500;
  const timeoutMs = opts.timeoutMs ?? 180_000;
  const { request_id } = await submitImage(input);

  const deadline = Date.now() + timeoutMs;
  for (;;) {
    if (opts.signal?.aborted) throw new ApiError('aborted', 'aborted', 0);
    const status = await getImageStatus(request_id);
    if (status.status === 'COMPLETED') {
      const { images } = await getImageResult(request_id);
      return images;
    }
    if (status.status === 'FAILED') {
      throw new ApiError('generation_failed', status.error ?? '生成失败', 500);
    }
    if (Date.now() > deadline) {
      throw new ApiError('timeout', '生成超时', 504);
    }
    await new Promise((r) => setTimeout(r, intervalMs));
  }
}
