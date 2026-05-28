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
