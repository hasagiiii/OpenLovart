import type { Metadata } from 'next';
import './globals.css';

import { AuthProvider } from '@/lib/auth/client';

export const metadata: Metadata = {
  title: 'OpenLovart',
  description: 'AI-powered design platform',
};

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html lang="en">
      <body className="antialiased">
        <AuthProvider>{children}</AuthProvider>
      </body>
    </html>
  );
}
