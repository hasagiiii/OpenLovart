'use client';

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';

export interface ClientUser {
  id: string;
  email: string;
  emailVerified: boolean;
}

interface AuthState {
  user: ClientUser | null;
  loading: boolean;
}

interface AuthContextValue extends AuthState {
  refresh: () => Promise<void>;
  signOut: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

async function fetchMe(): Promise<ClientUser | null> {
  const res = await fetch('/api/auth/me', {
    method: 'GET',
    credentials: 'include',
    cache: 'no-store',
  });
  if (res.status === 401) return null;
  if (!res.ok) throw new Error(`me_failed_${res.status}`);
  const data = (await res.json()) as {
    id: string;
    email: string;
    email_verified: boolean;
  };
  return {
    id: data.id,
    email: data.email,
    emailVerified: data.email_verified,
  };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ user: null, loading: true });
  const mounted = useRef(true);

  const refresh = useCallback(async () => {
    setState((s) => ({ ...s, loading: true }));
    try {
      const user = await fetchMe();
      if (mounted.current) setState({ user, loading: false });
    } catch {
      if (mounted.current) setState({ user: null, loading: false });
    }
  }, []);

  const signOut = useCallback(async () => {
    try {
      await fetch('/api/auth/logout', {
        method: 'POST',
        credentials: 'include',
      });
    } finally {
      if (mounted.current) setState({ user: null, loading: false });
    }
  }, []);

  useEffect(() => {
    mounted.current = true;
    void refresh();
    return () => {
      mounted.current = false;
    };
  }, [refresh]);

  const value = useMemo<AuthContextValue>(
    () => ({ ...state, refresh, signOut }),
    [state, refresh, signOut],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be called inside <AuthProvider>');
  }
  return ctx;
}

// ---------- Credential helpers ----------

interface AuthErrorBody {
  error?: string;
  message?: string;
}

export class AuthError extends Error {
  code: string;
  status: number;
  constructor(code: string, message: string, status: number) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

async function postJSON(path: string, body: unknown): Promise<Response> {
  const res = await fetch(path, {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    let parsed: AuthErrorBody = {};
    try {
      parsed = (await res.json()) as AuthErrorBody;
    } catch {
      // ignore — fall back to status text
    }
    throw new AuthError(
      parsed.error ?? 'request_failed',
      parsed.message ?? res.statusText,
      res.status,
    );
  }
  return res;
}

export async function signIn(email: string, password: string): Promise<void> {
  await postJSON('/api/auth/login', { email, password });
}

export async function signUp(email: string, password: string): Promise<void> {
  await postJSON('/api/auth/register', { email, password });
}

export async function requestPasswordReset(email: string): Promise<void> {
  await postJSON('/api/auth/forgot-password', { email });
}

export async function resetPassword(
  token: string,
  newPassword: string,
): Promise<void> {
  await postJSON('/api/auth/reset-password', { token, password: newPassword });
}

export async function resendVerificationEmail(): Promise<void> {
  await postJSON('/api/auth/verify-email/resend', {});
}

export function signInWithGoogle(next?: string): void {
  // Top-level navigation; the backend will redirect through Google and
  // ultimately back to /oidc/callback?status=...
  const url = new URL('/api/auth/oidc/google/start', window.location.origin);
  if (next) url.searchParams.set('next', next);
  window.location.assign(url.toString());
}
