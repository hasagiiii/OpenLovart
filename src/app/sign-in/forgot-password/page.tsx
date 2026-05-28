'use client';

import Link from 'next/link';
import { useState } from 'react';

import { AuthError, requestPasswordReset } from '@/lib/auth/client';

export default function ForgotPasswordPage() {
  const [email, setEmail] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await requestPasswordReset(email);
      // Anti-enumeration: backend returns 200 regardless of whether the
      // email exists. We always show the same confirmation screen.
      setDone(true);
    } catch (err) {
      if (err instanceof AuthError && err.code === 'rate_limited') {
        setError('尝试过于频繁，请稍后再试');
      } else {
        setError('请求失败，请稍后重试');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-6">
        {done ? (
          <>
            <h1 className="text-2xl font-semibold text-gray-900">查看你的邮箱</h1>
            <p className="text-sm text-gray-600">
              如果 <span className="font-medium">{email}</span> 在我们的系统中，
              我们已发送一封包含密码重置链接的邮件。点击链接以设置新密码。
            </p>
            <Link
              href="/sign-in"
              className="inline-block text-sm text-black hover:underline"
            >
              返回登录
            </Link>
          </>
        ) : (
          <>
            <div>
              <h1 className="text-2xl font-semibold text-gray-900">忘记密码</h1>
              <p className="mt-1 text-sm text-gray-500">
                输入注册邮箱，我们会向你发送密码重置链接
              </p>
            </div>
            <form className="space-y-4" onSubmit={handleSubmit}>
              <label className="block text-sm">
                <span className="text-gray-700">邮箱</span>
                <input
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  autoComplete="email"
                  required
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
                {submitting ? '提交中…' : '发送重置链接'}
              </button>
            </form>
            <Link
              href="/sign-in"
              className="block text-sm text-gray-600 hover:text-gray-900"
            >
              返回登录
            </Link>
          </>
        )}
      </div>
    </div>
  );
}
