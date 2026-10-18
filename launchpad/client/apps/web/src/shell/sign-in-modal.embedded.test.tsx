// @vitest-environment jsdom
import React, { act } from 'react';
import { afterAll, beforeAll, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { injected } from 'wagmi/connectors';
import { defineChain } from 'viem';

// The embedded-wallet flag is computed when wagmi-config is first imported, so
// the deployment settings go into the real environment before any app module
// loads. Supabase points at a closed local port: the SDK is constructed for
// real, and anything it tries to reach fails instead of hitting a service.
const ENV_KEYS = ['NEXT_PUBLIC_SUPABASE_URL', 'NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY'] as const;
const previous = Object.fromEntries(ENV_KEYS.map(key => [key, process.env[key]]));

let app: {
  SignInModal: typeof import('./sign-in-modal').default;
  NO_BROWSER_WALLET_NOTE: string;
  NO_BROWSER_WALLET_NOTE_WALLET_ONLY: string;
  embeddedWalletConfigured: boolean;
  WalletRuntimeProvider: typeof import('@/core/wallet-sdk/runtime').WalletRuntimeProvider;
  openAuthModal: typeof import('./auth-modal-store').openAuthModal;
  closeAuthModal: typeof import('./auth-modal-store').closeAuthModal;
};

beforeAll(async () => {
  (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
  process.env.NEXT_PUBLIC_SUPABASE_URL = 'http://127.0.0.1:1';
  process.env.NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY = 'sb_publishable_closed_local_boundary';
  const modal = await import('./sign-in-modal');
  const wagmi = await import('@/core/wagmi-config');
  const runtime = await import('@/core/wallet-sdk/runtime');
  const store = await import('./auth-modal-store');
  app = {
    SignInModal: modal.default,
    NO_BROWSER_WALLET_NOTE: modal.NO_BROWSER_WALLET_NOTE,
    NO_BROWSER_WALLET_NOTE_WALLET_ONLY: modal.NO_BROWSER_WALLET_NOTE_WALLET_ONLY,
    embeddedWalletConfigured: wagmi.embeddedWalletConfigured,
    WalletRuntimeProvider: runtime.WalletRuntimeProvider,
    openAuthModal: store.openAuthModal,
    closeAuthModal: store.closeAuthModal,
  };
});

afterAll(() => {
  for (const key of ENV_KEYS) {
    if (previous[key] === undefined) delete process.env[key];
    else process.env[key] = previous[key];
  }
});

const chain = defineChain({ id: 125, name: 'Closed local boundary', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });

it('with the embedded wallet configured and no browser wallet, the note points at the embedded wallet', async () => {
  expect(app.embeddedWalletConfigured).toBe(true);
  delete (window as unknown as { ethereum?: unknown }).ethereum;
  const config = createConfig({ chains: [chain], connectors: [injected()], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: true, multiInjectedProviderDiscovery: false });
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root: Root = createRoot(container);
  const { SignInModal, WalletRuntimeProvider } = app;
  act(() => {
    app.openAuthModal();
    root.render(
      <WagmiProvider config={config}>
        <QueryClientProvider client={queries}>
          <WalletRuntimeProvider><SignInModal /></WalletRuntimeProvider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  });
  const dialog = () => document.body.querySelector<HTMLElement>('[role="dialog"]');
  // First synchronous render: no note yet, whatever the deployment offers.
  expect(dialog()?.textContent).not.toContain('No browser wallet found');
  await act(async () => { for (let i = 0; i < 5; i++) await new Promise(resolve => setTimeout(resolve, 0)); });
  const text = dialog()?.textContent ?? '';
  expect(text).toContain(app.NO_BROWSER_WALLET_NOTE);
  expect(text).not.toContain(app.NO_BROWSER_WALLET_NOTE_WALLET_ONLY);
  expect(text).not.toContain('Injected');
  act(() => root.unmount());
  container.remove();
  queries.clear();
  act(() => app.closeAuthModal());
});
