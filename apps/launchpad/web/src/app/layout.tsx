import type { Metadata, Viewport } from 'next';
import type { ReactNode } from 'react';
import { headers } from 'next/headers';
import { cookieToInitialState } from 'wagmi';
import { createWagmiConfig } from '@/core/wagmi-config';
import AppProviders from '@/ui/providers/app-providers';
import AppShell from '@/shell/app-shell';
import PlatformRulesModal from '@/ui/shared/platform-rules-modal';
import CookieBanner from '@/ui/shared/cookie-banner';
import { BackendConfigurationError } from '@/ui/shared/backend-configuration-error';
import { backendConfigurationProblems } from '@/core/backend-origins';
import './globals.css';

const privateFixtureProfileInput = process.env.NEXT_PUBLIC_PRIVATE_QUALIFICATION_AUTHORITY_PROFILE;

export const metadata: Metadata = {
  title: { default: 'Sidiora Launch', template: '%s | Sidiora Launch' },
  description: 'Launch and trade coins on Paxeer',
  icons: {
    icon: '/Kindle-Launch-logo-dark.png',
    shortcut: '/Kindle-Launch-logo-dark.png',
    apple: '/Kindle-Launch-logo-dark.png',
  },
  manifest: '/manifest.json',
};

export const viewport: Viewport = {
  width: 'device-width',
  initialScale: 1,
  viewportFit: 'cover',
  themeColor: '#07090D',
};

export default async function RootLayout({ children }: { children: ReactNode }) {
  // A production build without its backend origins renders one explicit error
  // state and mounts nothing that would contact a backend (no fallback host).
  const configurationProblems = backendConfigurationProblems();
  if (configurationProblems.length > 0) {
    console.error(`[sidiora] backend origins are not configured: ${configurationProblems.map(problem => problem.message).join('; ')}`);
    return (
      <html lang="en">
        <body className="bg-canvas text-text">
          <BackendConfigurationError problems={configurationProblems} />
        </body>
      </html>
    );
  }
  const hdrs = await headers();
  const cookie = hdrs.get('cookie') ?? '';
  const wagmiInitialState = cookieToInitialState(createWagmiConfig(), cookie);

  return (
    <html lang="en">
      <body className="bg-canvas text-text">
        <PlatformRulesModal />
        <CookieBanner />
        <AppProviders wagmiInitialState={wagmiInitialState} privateFixtureProfileInput={privateFixtureProfileInput}>
          <AppShell>
            {children}
          </AppShell>
        </AppProviders>
      </body>
    </html>
  );
}
