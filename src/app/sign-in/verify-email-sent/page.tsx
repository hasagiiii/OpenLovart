'use client';

import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { Suspense } from 'react';

function Body() {
  const params = useSearchParams();
  const email = params.get('email');
  return (
    <div className="min-h-screen flex items-center justify-center bg-gray-50 px-4">
      <div className="w-full max-w-md bg-white rounded-xl shadow-sm border border-gray-200 p-8 space-y-4 text-center">
        <h1 className="text-2xl font-semibold text-gray-900">查看你的邮箱</h1>
        <p className="text-sm text-gray-600">
          {email ? (
            <>
              我们已向 <span className="font-medium">{email}</span>{' '}
              发送了一封验证邮件，点击邮件中的链接以激活账户。
            </>
          ) : (
            <>我们已向你的邮箱发送了一封验证邮件，点击链接以激活账户。</>
          )}
        </p>
        <p className="text-xs text-gray-500">
          没收到？检查垃圾邮件文件夹，或稍后再试。
        </p>
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

export default function VerifyEmailSentPage() {
  return (
    <Suspense fallback={null}>
      <Body />
    </Suspense>
  );
}
