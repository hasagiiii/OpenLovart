'use client';

import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { Suspense, useState } from 'react';

import { AuthError, signIn, signInWithGoogle } from '@/lib/auth/client';

function SignInForm() {
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
      await signIn(email, password);
      router.replace(next);
    } catch (err) {
      if (err instanceof AuthError) {
        setError(messageForCode(err.code));
      } else {
        setError('登录失败，请稍后重试');
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-6">
        <div>
          <h1 className="text-2xl font-semibold text-gray-900">登录</h1>
          <p className="mt-1 text-sm text-gray-500">
            使用邮箱和密码登录 OpenLovart
          </p>
        </div>

        <form className="space-y-4" onSubmit={handleSubmit}>
          <Field
            label="邮箱"
            type="email"
            value={email}
            onChange={setEmail}
            autoComplete="email"
            required
          />
          <Field
            label="密码"
            type="password"
            value={password}
            onChange={setPassword}
            autoComplete="current-password"
            required
          />
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
            {submitting ? '登录中…' : '登录'}
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
          使用 Google 登录
        </button>

        <div className="flex justify-between text-sm">
          <Link
            href="/sign-in/forgot-password"
            className="text-gray-600 hover:text-gray-900"
          >
            忘记密码？
          </Link>
          <Link
            href={`/sign-up${next ? `?next=${encodeURIComponent(next)}` : ''}`}
            className="text-gray-600 hover:text-gray-900"
          >
            创建账户
          </Link>
        </div>
      </div>
    </div>
  );
}

function Field({
  label,
  type,
  value,
  onChange,
  autoComplete,
  required,
}: {
  label: string;
  type: string;
  value: string;
  onChange: (v: string) => void;
  autoComplete?: string;
  required?: boolean;
}) {
  return (
    <label className="block text-sm">
      <span className="text-gray-700">{label}</span>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete={autoComplete}
        required={required}
        className="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-gray-900 focus:border-black focus:outline-none"
      />
    </label>
  );
}

function messageForCode(code: string): string {
  switch (code) {
    case 'invalid_credentials':
      return '邮箱或密码不正确';
    case 'rate_limited':
      return '尝试过于频繁，请稍后再试';
    case 'email_not_verified':
      return '请先到邮箱完成验证后再登录';
    default:
      return '登录失败，请稍后重试';
  }
}

export default function SignInPage() {
  return (
    <Suspense fallback={null}>
      <SignInForm />
    </Suspense>
  );
}
