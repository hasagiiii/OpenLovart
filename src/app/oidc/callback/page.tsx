'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useEffect } from 'react';

// The OIDC callback ultimately redirects via the backend to an in-app
// destination. This page is reached only when something went wrong (the
// backend redirected here with a status query) or as a transient landing
// page when the redirect itself was preserved on the frontend.
function CallbackBody() {
  const params = useSearchParams();
  const router = useRouter();
  const status = params.get('status');
  const next = params.get('next');

  useEffect(() => {
    if (!status || status === 'ok') {
      router.replace(next && next.startsWith('/') ? next : '/lovart');
    }
  }, [status, next, router]);

  if (!status || status === 'ok') {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-50">
        <p className="text-sm text-gray-600">登录成功，正在跳转…</p>
      </div>
    );
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-4 text-center">
        <h1 className="text-2xl font-semibold text-gray-900">登录失败</h1>
        <p className="text-sm text-gray-600">{messageForStatus(status)}</p>
        <Link
          href="/sign-in"
          className="inline-block text-sm text-black hover:underline"
        >
          返回登录
        </Link>
      </div>
    </div>
  );
}

function messageForStatus(status: string): string {
  switch (status) {
    case 'state_mismatch':
    case 'state_invalid':
    case 'missing_state':
      return '会话状态校验失败，请重新发起 Google 登录。';
    case 'oidc_exchange_failed':
      return '与 Google 通信失败，请稍后再试。';
    case 'email_not_verified':
      return '你的 Google 账户邮箱尚未验证，请先在 Google 完成验证。';
    case 'rate_limited':
      return '尝试过于频繁，请稍后再试。';
    default:
      return '出了点问题，请重新尝试登录。';
  }
}

export default function OidcCallbackPage() {
  return (
    <Suspense fallback={null}>
      <CallbackBody />
    </Suspense>
  );
}
