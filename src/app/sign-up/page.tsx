'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useState } from 'react';

import { AuthError, signInWithGoogle, signUp } from '@/lib/auth/client';

function SignUpForm() {
  const router = useRouter();
  const params = useSearchParams();
  const next = params.get('next') ?? '/lovart';

  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await signUp(email, password);
      // Backend silently 200s on duplicate email (anti-enumeration). In both
      // "fresh user" and "duplicate" cases we route to the verify-email-sent
      // page with the email pre-filled.
      router.replace(
        `/sign-in/verify-email-sent?email=${encodeURIComponent(email)}`,
      );
    } catch (err) {
      if (err instanceof AuthError) {
        setError(messageForCode(err.code));
      } else {
        setError('注册失败，请稍后重试');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-6">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">创建账户</h1>
          <p className="mt-1 text-sm text-gray-500">
            注册后我们会向你发送邮箱验证邮件
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
          <label className="block text-sm">
            <span className="text-gray-700">密码</span>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
              minLength={10}
              className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-gray-900 focus:border-black focus:outline-none"
            />
            <span className="mt-1 block text-xs text-gray-500">
              密码至少 10 个字符
            </span>
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
            {submitting ? '注册中…' : '注册'}
          </button>
        </form>

        <div className="relative">
          <div className="absolute inset-0 flex items-center">
            <div className="w-full border-t border-gray-200" />
          </div>
          <div className="relative flex justify-center text-xs uppercase">
            <span className="bg-white px-2 text-gray-400">或</span>
          </div>
        </div>

        <button
          type="button"
          onClick={() => signInWithGoogle(next)}
          className="w-full border border-gray-300 rounded-md py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
        >
          使用 Google 注册
        </button>

        <p className="text-sm text-gray-600">
          已有账户？{' '}
          <Link
            href={`/sign-in${next ? `?next=${encodeURIComponent(next)}` : ''}`}
            className="text-black hover:underline"
          >
            立即登录
          </Link>
        </p>
      </div>
    </div>
  );
}

function messageForCode(code: string): string {
  switch (code) {
    case 'invalid_request':
      return '邮箱或密码格式不正确';
    case 'rate_limited':
      return '尝试过于频繁，请稍后再试';
    case 'weak_password':
      return '密码强度不足，至少需要 10 个字符';
    default:
      return '注册失败，请稍后重试';
  }
}

export default function SignUpPage() {
  return (
    <Suspense fallback={null}>
      <SignUpForm />
    </Suspense>
  );
}
