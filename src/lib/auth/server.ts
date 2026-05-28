import 'server-only';

import { cookies } from 'next/headers';
import { createRemoteJWKSet, jwtVerify, type JWTPayload } from 'jose';

const BACKEND_INTERNAL_URL =
  process.env.BACKEND_INTERNAL_URL ?? 'http://localhost:8080';

const JWKS_URL = new URL('/api/auth/.well-known/jwks.json', BACKEND_INTERNAL_URL);

const JWKS_CACHE_TTL_MS = (() => {
  const raw = process.env.AUTH_JWKS_CACHE_TTL;
  const parsed = raw ? Number.parseInt(raw, 10) : 300;
  if (!Number.isFinite(parsed) || parsed <= 0) return 300_000;
  return parsed * 1000;
})();

// Module-scoped JWKS so each server process keeps a single in-memory key set
// that auto-refreshes per `cooldownDuration`. NEVER read or store any signing
// secret here — verification is asymmetric and only needs public keys.
const jwks = createRemoteJWKSet(JWKS_URL, {
  cooldownDuration: JWKS_CACHE_TTL_MS,
  cacheMaxAge: JWKS_CACHE_TTL_MS,
});

export interface SessionUser {
  id: string;
  email: string;
  emailVerified: boolean;
}

export interface ServerSession {
  user: SessionUser;
  claims: JWTPayload;
}

export async function getServerSession(): Promise<ServerSession | null> {
  const store = await cookies();
  // The backend writes the cookie as `__Host-access` when secure mode is on
  // and `access` otherwise; check both so dev/prod behave the same.
  const token =
    store.get('__Host-access')?.value ?? store.get('access')?.value ?? null;
  if (!token) return null;

  try {
    const { payload } = await jwtVerify(token, jwks, {
      algorithms: ['RS256'],
    });
    if (!payload.sub || typeof payload.sub !== 'string') return null;
    const email =
      typeof payload.email === 'string' ? payload.email : '';
    const emailVerified =
      typeof payload.email_verified === 'boolean'
        ? payload.email_verified
        : false;
    return {
      user: {
        id: payload.sub,
        email,
        emailVerified,
      },
      claims: payload,
    };
  } catch {
    return null;
  }
}
