'use client';

import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { Suspense, useEffect, useState } from 'react';

type Status = 'pending' | 'success' | 'expired' | 'invalid' | 'error';

function VerifyBody() {
  const params = useSearchParams();
  const token = params.get('token');
  const [status, setStatus] = useState<Status>(token ? 'pending' : 'invalid');

  useEffect(() => {
    if (!token) return;
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch(
          `/api/auth/verify-email?token=${encodeURIComponent(token)}`,
          {
            method: 'GET',
            credentials: 'include',
          },
        );
        if (cancelled) return;
        if (res.ok) {
          setStatus('success');
          return;
        }
        let code = '';
        try {
          const body = (await res.json()) as { error?: string };
          code = body.error ?? '';
        } catch {
          // ignore
        }
        if (code === 'token_expired') setStatus('expired');
        else if (code === 'token_invalid') setStatus('invalid');
        else setStatus('error');
      } catch {
        if (!cancelled) setStatus('error');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [token]);

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-4 text-center">
        {status === 'pending' && (
          <>
            <h1 className="text-2xl font-semibold text-gray-900">验证中…</h1>
            <p className="text-sm text-gray-600">请稍候</p>
          </>
        )}
        {status === 'success' && (
          <>
            <h1 className="text-2xl font-semibold text-gray-900">邮箱已验证</h1>
            <p className="text-sm text-gray-600">
              你现在可以登录使用 OpenLovart 了。
            </p>
            <Link
              href="/sign-in"
              className="inline-block text-sm text-black hover:underline"
            >
              前往登录
            </Link>
          </>
        )}
        {status === 'expired' && (
          <>
            <h1 className="text-2xl font-semibold text-gray-900">链接已过期</h1>
            <p className="text-sm text-gray-600">
              请重新登录后申请新的验证邮件。
            </p>
            <Link
              href="/sign-in"
              className="inline-block text-sm text-black hover:underline"
            >
              返回登录
            </Link>
          </>
        )}
        {status === 'invalid' && (
          <>
            <h1 className="text-2xl font-semibold text-gray-900">链接无效</h1>
            <p className="text-sm text-gray-600">
              该验证链接不正确或已被使用。
            </p>
            <Link
              href="/sign-in"
              className="inline-block text-sm text-black hover:underline"
            >
              返回登录
            </Link>
          </>
        )}
        {status === 'error' && (
          <>
            <h1 className="text-2xl font-semibold text-gray-900">验证失败</h1>
            <p className="text-sm text-gray-600">
              出了点问题，请稍后重试或联系支持。
            </p>
            <Link
              href="/sign-in"
              className="inline-block text-sm text-black hover:underline"
            >
              返回登录
            </Link>
          </>
        )}
      </div>
    </div>
  );
}

export default function VerifyEmailPage() {
  return (
    <Suspense fallback={null}>
      <VerifyBody />
    </Suspense>
  );
}
