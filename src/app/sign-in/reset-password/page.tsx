'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useState } from 'react';

import { AuthError, resetPassword } from '@/lib/auth/client';

function ResetForm() {
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get('token') ?? '';

  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);

    if (!token) {
      setError('链接无效或已过期，请重新申请');
      return;
    }
    if (password !== confirm) {
      setError('两次输入的密码不一致');
      return;
    }

    setSubmitting(true);
    try {
      await resetPassword(token, password);
      router.replace('/sign-in?reset=success');
    } catch (err) {
      if (err instanceof AuthError) {
        switch (err.code) {
          case 'token_invalid':
          case 'token_expired':
            setError('链接无效或已过期，请重新申请');
            break;
          case 'weak_password':
            setError('密码强度不足，至少需要 10 个字符');
            break;
          default:
            setError('重置失败，请稍后重试');
        }
      } else {
        setError('重置失败，请稍后重试');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-6">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">设置新密码</h1>
          <p className="mt-1 text-sm text-gray-500">
            重置后，你之前的所有登录会话都会被注销
          </p>
        </div>
        <form className="space-y-4" onSubmit={handleSubmit}>
          <label className="block text-sm">
            <span className="text-gray-700">新密码</span>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
              minLength={10}
              className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-gray-900 focus:border-black focus:outline-none"
            />
          </label>
          <label className="block text-sm">
            <span className="text-gray-700">确认新密码</span>
            <input
              type="password"
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
              autoComplete="new-password"
              required
              minLength={10}
              className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-gray-900 focus:border-black focus:outline-none"
            />
          </label>
          {error ? (
            <p className="text-sm text-red-600" role="alert">
              {error}
            </p>
          ) : null}
          <button
            type="submit"
            disabled={submitting}
            className="w-full bg-black text-white rounded-md py-2 text-sm font-medium hover:bg-gray-800 disabled:opacity-50"
          >
            {submitting ? '提交中…' : '设置新密码'}
          </button>
        </form>
        <Link
          href="/sign-in"
          className="block text-sm text-gray-600 hover:text-gray-900"
        >
          返回登录
        </Link>
      </div>
    </div>
  );
}

export default function ResetPasswordPage() {
  return (
    <Suspense fallback={null}>
      <ResetForm />
    </Suspense>
  );
}
