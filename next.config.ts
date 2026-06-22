import type { NextConfig } from 'next';

// Dev rewrites: forward `/api/auth/*`, `/api/projects/*`, `/api/credits/*`,
// `/api/ai/*` to the local Go backend so the Next.js app can call them via
// same-origin fetch (cookies + CSRF stay simple). In production we expect the
// frontend and backend to be served from the same hostname (or the deployer
// can re-create equivalent rewrites at the edge).
const isDev = process.env.NODE_ENV === 'development';
const BACKEND_DEV_URL =
  process.env.BACKEND_DEV_URL ?? 'http://localhost:8080';

const nextConfig: NextConfig = {
  async rewrites() {
    if (!isDev) return [];
    return [
      { source: '/api/auth/:path*', destination: `${BACKEND_DEV_URL}/api/auth/:path*` },
      { source: '/api/projects/:path*', destination: `${BACKEND_DEV_URL}/api/projects/:path*` },
      { source: '/api/projects', destination: `${BACKEND_DEV_URL}/api/projects` },
      { source: '/api/credits', destination: `${BACKEND_DEV_URL}/api/credits` },
      { source: '/api/ai/:path*', destination: `${BACKEND_DEV_URL}/api/ai/:path*` },
      { source: '/api/health', destination: `${BACKEND_DEV_URL}/api/health` },
    ];
  },
};

export default nextConfig;
