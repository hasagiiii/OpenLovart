import { NextResponse, type NextRequest } from 'next/server';
import { createRemoteJWKSet, jwtVerify } from 'jose';

// JWKS verifier — module-scoped so the runtime keeps a single key set with
// auto-refresh. Edge runtime supports `jose` natively.
const BACKEND_INTERNAL_URL =
  process.env.BACKEND_INTERNAL_URL ?? 'http://localhost:8080';
const JWKS_URL = new URL(
  '/api/auth/.well-known/jwks.json',
  BACKEND_INTERNAL_URL,
);
const JWKS_CACHE_TTL_MS = (() => {
  const raw = process.env.AUTH_JWKS_CACHE_TTL;
  const parsed = raw ? Number.parseInt(raw, 10) : 300;
  if (!Number.isFinite(parsed) || parsed <= 0) return 300_000;
  return parsed * 1000;
})();
const jwks = createRemoteJWKSet(JWKS_URL, {
  cooldownDuration: JWKS_CACHE_TTL_MS,
  cacheMaxAge: JWKS_CACHE_TTL_MS,
});

// Routes that always pass the guard.
const PUBLIC_PREFIXES = [
  '/sign-in',
  '/sign-up',
  '/oidc/callback',
  '/api/auth/',
  '/api/health',
];

function isPublic(path: string): boolean {
  if (path === '/') return true;
  for (const prefix of PUBLIC_PREFIXES) {
    if (path === prefix || path.startsWith(prefix)) return true;
  }
  return false;
}

// Only `/lovart` (and subroutes) requires authentication today. Other pages
// are public.
function isProtected(path: string): boolean {
  return path === '/lovart' || path.startsWith('/lovart/');
}

export async function middleware(request: NextRequest): Promise<NextResponse> {
  const { pathname } = request.nextUrl;

  if (!isProtected(pathname) || isPublic(pathname)) {
    return NextResponse.next();
  }

  const token =
    request.cookies.get('__Host-access')?.value ??
    request.cookies.get('access')?.value ??
    null;

  if (token) {
    try {
      await jwtVerify(token, jwks, { algorithms: ['RS256'] });
      return NextResponse.next();
    } catch {
      // fall through to redirect
    }
  }

  const url = request.nextUrl.clone();
  url.pathname = '/sign-in';
  url.search = `?next=${encodeURIComponent(pathname + request.nextUrl.search)}`;
  return NextResponse.redirect(url);
}

export const config = {
  matcher: [
    // Skip Next.js internals and static assets; run for everything else.
    '/((?!_next/static|_next/image|favicon.ico|.*\\.(?:svg|png|jpg|jpeg|gif|webp|ico|css|js|map|webmanifest)$).*)',
  ],
};
