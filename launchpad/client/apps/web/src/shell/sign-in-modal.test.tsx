// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { createRoot, hydrateRoot, type Root } from 'react-dom/client';
import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { injected } from 'wagmi/connectors';
import { defineChain } from 'viem';
import { WalletRuntimeProvider } from '@/core/wallet-sdk/runtime';
import SignInModal, { NO_BROWSER_WALLET_NOTE, NO_BROWSER_WALLET_NOTE_WALLET_ONLY } from './sign-in-modal';
import { embeddedWalletConfigured } from '@/core/wagmi-config';
import { closeAuthModal, openAuthModal } from './auth-modal-store';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

// Real wagmi config with the app's real connector list shape (the generic
// injected connector), real react-query and the real wallet runtime. The RPC
// points at a closed local port so no chain read can succeed by accident.
const chain = defineChain({ id: 125, name: 'Closed local boundary', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const makeConfig = () => createConfig({ chains: [chain], connectors: [injected()], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: true, multiInjectedProviderDiscovery: false });

const ACCOUNT = '0x1111111111111111111111111111111111111111';

/** A small real EIP-1193 provider: answers the requests an injected connect makes and records them. */
class TestBrowserWallet {
  readonly isRabby = true;
  readonly isMetaMask = true;
  readonly calls: string[] = [];
  private readonly listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  async request({ method }: { method: string; params?: unknown }): Promise<unknown> {
    this.calls.push(method);
    switch (method) {
      case 'eth_chainId': return '0x7d';
      case 'eth_accounts':
      case 'eth_requestAccounts': return [ACCOUNT];
      case 'wallet_requestPermissions': return [{ parentCapability: 'eth_accounts', caveats: [{ type: 'restrictReturnedAccounts', value: [ACCOUNT] }] }];
      default: throw Object.assign(new Error(`Unsupported method ${method}`), { code: 4200 });
    }
  }
  on(event: string, listener: (...args: unknown[]) => void) { (this.listeners.get(event) ?? this.listeners.set(event, new Set()).get(event)!).add(listener); return this; }
  removeListener(event: string, listener: (...args: unknown[]) => void) { this.listeners.get(event)?.delete(listener); return this; }
}

type EthereumWindow = { ethereum?: unknown };
const installWallet = (wallet: unknown) => { (window as unknown as EthereumWindow).ethereum = wallet; };
const removeWallet = () => { delete (window as unknown as EthereumWindow).ethereum; };

const mounted: Array<{ root: Root; container: HTMLElement; queries: QueryClient }> = [];

function Tree({ config, queries }: { config: ReturnType<typeof makeConfig>; queries: QueryClient }) {
  return (
    <WagmiProvider config={config}>
      <QueryClientProvider client={queries}>
        <WalletRuntimeProvider>
          <SignInModal />
        </WalletRuntimeProvider>
      </QueryClientProvider>
    </WagmiProvider>
  );
}

/** Mounts the real modal and opens it, returning the DOM as committed by that first synchronous pass (no async probe has resolved yet). */
function mountOpen() {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const root = createRoot(container);
  const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  act(() => { openAuthModal(); root.render(<Tree config={makeConfig()} queries={queries} />); });
  mounted.push({ root, container, queries });
  return dialogText();
}

const dialog = () => document.body.querySelector<HTMLElement>('[role="dialog"]');
const dialogText = () => dialog()?.textContent ?? '';
const walletButtons = () => Array.from(dialog()?.querySelectorAll<HTMLButtonElement>('[aria-label="Wallets"] button') ?? []);
/** Lets the provider probe (an awaited getProvider) settle and React commit it. */
const settle = () => act(async () => { for (let i = 0; i < 5; i++) await new Promise(resolve => setTimeout(resolve, 0)); });

afterEach(() => {
  for (const { root, container, queries } of mounted.splice(0)) {
    act(() => root.unmount());
    container.remove();
    queries.clear();
  }
  act(() => closeAuthModal());
  removeWallet();
  document.cookie.split(';').forEach(cookie => { document.cookie = `${cookie.split('=')[0]!.trim()}=; expires=Thu, 01 Jan 1970 00:00:00 GMT`; });
  try { localStorage.clear(); } catch { /* storage optional */ }
});

describe('SignInModal browser-wallet detection', () => {
  it('without an injected provider: no "Injected" option, shows the note, and nothing connects', async () => {
    removeWallet();
    mountOpen();
    // While the probe runs, the neutral row is disabled; clicking it does nothing.
    const probingRow = walletButtons()[0]!;
    expect(probingRow.disabled).toBe(true);
    await act(async () => { probingRow.click(); });
    await settle();
    const text = dialogText();
    expect(dialog()).not.toBeNull();
    expect(text).not.toContain('Injected');
    expect(text).not.toContain('Browser wallet');
    // This environment has no Supabase settings, so the embedded wallet is not
    // offered and the note must not point at it.
    expect(embeddedWalletConfigured).toBe(false);
    expect(text).toContain(NO_BROWSER_WALLET_NOTE_WALLET_ONLY);
    expect(text).not.toContain(NO_BROWSER_WALLET_NOTE);
    expect(walletButtons()).toHaveLength(0);
    expect(dialog()!.querySelectorAll('[role="alert"]')).toHaveLength(0);
    expect(text).not.toContain("Couldn't connect");
    expect(text).not.toContain('Waiting for your wallet');
  });

  it('with a real EIP-1193 provider on window: names the wallet and clicking connects through it', async () => {
    const wallet = new TestBrowserWallet();
    installWallet(wallet);
    mountOpen();
    await settle();
    const text = dialogText();
    expect(text).not.toContain('Injected');
    expect(text).not.toContain('No browser wallet found');
    const buttons = walletButtons();
    expect(buttons).toHaveLength(1);
    expect(buttons[0]!.textContent).toContain('Rabby');
    expect(buttons[0]!.disabled).toBe(false);

    await act(async () => { buttons[0]!.click(); });
    await settle();
    expect(wallet.calls).toContain('wallet_requestPermissions');
    expect(wallet.calls).toContain('eth_chainId');
    // Connected on chain 125, so the dialog moves to the signing step, still naming the detected wallet.
    expect(dialogText()).toContain('Sign in with Rabby');
    expect(dialogText()).not.toContain("Couldn't connect");
  });

  it('reports a user rejection as a cancelled request, not a generic failure', async () => {
    const wallet = new TestBrowserWallet();
    wallet.request = async ({ method }) => {
      wallet.calls.push(method);
      if (method === 'eth_chainId') return '0x7d';
      if (method === 'eth_accounts') return [];
      throw Object.assign(new Error('User rejected the request.'), { code: 4001 });
    };
    installWallet(wallet);
    mountOpen();
    await settle();
    await act(async () => { walletButtons()[0]!.click(); });
    await settle();
    expect(dialog()!.querySelector('[role="alert"]')?.textContent).toBe('Connection cancelled');
  });

  it('first client render has no window-dependent output', async () => {
    removeWallet();
    const withoutWallet = mountOpen();
    for (const { root, container } of mounted.splice(0)) { act(() => root.unmount()); container.remove(); }
    act(() => closeAuthModal());

    installWallet(new TestBrowserWallet());
    const withWallet = mountOpen();
    // Before the post-mount probe resolves, both renders are identical: the
    // injected row is the neutral, disabled "Browser wallet" and no note shows.
    expect(withWallet).toBe(withoutWallet);
    expect(withWallet).toContain('Browser wallet');
    expect(withWallet).not.toContain('No browser wallet found');
    expect(walletButtons()[0]!.disabled).toBe(true);
    await settle();
    expect(dialogText()).toContain('Rabby');
  });

  it('hydrates server markup without mismatch while a browser wallet is installed', async () => {
    installWallet(new TestBrowserWallet());
    openAuthModal();
    const serverQueries = new QueryClient();
    const html = renderToString(<Tree config={makeConfig()} queries={serverQueries} />);
    serverQueries.clear();
    const container = document.createElement('div');
    container.innerHTML = html;
    document.body.appendChild(container);
    const errors: unknown[] = [];
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let root!: Root;
    await act(async () => {
      root = hydrateRoot(container, <Tree config={makeConfig()} queries={queries} />, { onRecoverableError: error => errors.push(error) });
    });
    mounted.push({ root, container, queries });
    await settle();
    expect(errors).toEqual([]);
    expect(dialogText()).toContain('Rabby');
  });
});
