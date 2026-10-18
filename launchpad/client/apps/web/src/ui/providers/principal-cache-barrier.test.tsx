// @vitest-environment jsdom
import React, { act } from 'react';
import { afterEach, beforeAll, describe, expect, it } from 'vitest';
import { createRoot, type Root } from 'react-dom/client';
import { renderToString } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { connect, disconnect } from 'wagmi/actions';
import { injected } from 'wagmi/connectors';
import { defineChain } from 'viem';
import { WalletRuntimeProvider, useWalletRuntime } from '@/core/wallet-sdk/runtime';
import { PrincipalCacheBarrier, principalBarrierBlocks } from './query-provider';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

// Real wagmi config (generic injected connector, closed RPC port), real
// react-query, the real wallet runtime and the real barrier.
const chain = defineChain({ id: 125, name: 'Closed local boundary', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const makeConfig = () => createConfig({ chains: [chain], connectors: [injected()], transports: { 125: http('http://127.0.0.1:1', { retryCount: 0, timeout: 500 }) }, ssr: true, multiInjectedProviderDiscovery: false });
const ACCOUNT = '0x1111111111111111111111111111111111111111';

/** A small real EIP-1193 provider answering what an injected connect asks. */
class TestBrowserWallet {
  private readonly listeners = new Map<string, Set<(...args: unknown[]) => void>>();
  async request({ method }: { method: string }): Promise<unknown> {
    switch (method) {
      case 'eth_chainId': return '0x7d';
      case 'eth_accounts':
      case 'eth_requestAccounts': return [ACCOUNT];
      case 'wallet_requestPermissions': return [{ parentCapability: 'eth_accounts', caveats: [{ type: 'restrictReturnedAccounts', value: [ACCOUNT] }] }];
      case 'wallet_revokePermissions': return null;
      default: throw Object.assign(new Error(`Unsupported method ${method}`), { code: 4200 });
    }
  }
  on(event: string, listener: (...args: unknown[]) => void) { (this.listeners.get(event) ?? this.listeners.set(event, new Set()).get(event)!).add(listener); return this; }
  removeListener(event: string, listener: (...args: unknown[]) => void) { this.listeners.get(event)?.delete(listener); return this; }
}
type EthereumWindow = { ethereum?: unknown };

function SessionProbe() {
  const runtime = useWalletRuntime();
  return <button type="button" data-testid="probe">{runtime.session?.account.toLowerCase() ?? 'anonymous'}</button>;
}
function Tree({ config, queries }: { config: ReturnType<typeof makeConfig>; queries: QueryClient }) {
  return (
    <WagmiProvider config={config}>
      <QueryClientProvider client={queries}>
        <WalletRuntimeProvider>
          <PrincipalCacheBarrier><SessionProbe /></PrincipalCacheBarrier>
        </WalletRuntimeProvider>
      </QueryClientProvider>
    </WagmiProvider>
  );
}

const mounted: Array<{ root: Root; container: HTMLElement; observer: MutationObserver }> = [];
afterEach(() => {
  for (const { root, container, observer } of mounted.splice(0)) { observer.disconnect(); act(() => root.unmount()); container.remove(); }
  delete (window as unknown as EthereumWindow).ethereum;
  try { localStorage.clear(); } catch { /* storage optional */ }
});
const settle = () => act(async () => { for (let i = 0; i < 10; i++) await new Promise(resolve => setTimeout(resolve, 0)); });

/** Mounts the tree and records every commit that made the app inert. */
function mount(config: ReturnType<typeof makeConfig>, queries: QueryClient) {
  const container = document.createElement('div');
  document.body.appendChild(container);
  const blocked: string[] = [];
  const observer = new MutationObserver(records => {
    for (const record of records) if (record.target instanceof HTMLElement && record.target.hasAttribute('inert')) blocked.push(container.textContent ?? '');
  });
  observer.observe(container, { subtree: true, attributes: true, attributeFilter: ['inert'], childList: true });
  const root = createRoot(container);
  act(() => { root.render(<Tree config={config} queries={queries} />); });
  mounted.push({ root, container, observer });
  return { container, blocked };
}
const probe = (container: HTMLElement) => container.querySelector<HTMLButtonElement>('[data-testid="probe"]')!;
const inert = (container: HTMLElement) => probe(container).parentElement!.hasAttribute('inert') || probe(container).parentElement!.getAttribute('aria-hidden') === 'true';

describe('PrincipalCacheBarrier', () => {
  it('server-renders the app visible and interactive', () => {
    const html = renderToString(<Tree config={makeConfig()} queries={new QueryClient()} />);
    expect(html).toContain('anonymous');
    expect(html).not.toContain('inert');
    expect(html).not.toContain('visibility:hidden');
    expect(html).not.toContain('Loading...');
  });

  it('never makes the app inert on first mount or when a wallet session starts', async () => {
    (window as unknown as EthereumWindow).ethereum = new TestBrowserWallet();
    const config = makeConfig();
    const { container, blocked } = mount(config, new QueryClient({ defaultOptions: { queries: { retry: false } } }));
    // First synchronous commit: already visible and interactive.
    expect(inert(container)).toBe(false);
    expect(container.textContent).not.toContain('Loading...');
    await settle();
    await act(async () => { await connect(config, { connector: config.connectors[0]! }); });
    await settle();
    expect(probe(container).textContent).toBe(ACCOUNT);
    expect(inert(container)).toBe(false);
    expect(blocked).toEqual([]);
  });

  it('hides the app while it clears the private cache of a principal that signed out, then restores it', async () => {
    (window as unknown as EthereumWindow).ethereum = new TestBrowserWallet();
    const config = makeConfig();
    const queries = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const { container, blocked } = mount(config, queries);
    await act(async () => { await connect(config, { connector: config.connectors[0]! }); });
    await settle();
    expect(probe(container).textContent).toBe(ACCOUNT);
    queries.setQueryData(['portfolio', ACCOUNT], { private: true });
    await act(async () => { await disconnect(config); });
    await settle();
    expect(blocked.length).toBeGreaterThan(0);
    expect(blocked.every(text => text.includes('Loading...'))).toBe(true);
    expect(queries.getQueryData(['portfolio', ACCOUNT])).toBeUndefined();
    expect(probe(container).textContent).toBe('anonymous');
    expect(inert(container)).toBe(false);
    expect(container.textContent).not.toContain('Loading...');
  });

  it('blocks only when leaving a published signed-in principal, on failure, or over an inherited private cache', () => {
    const anon = { key: 'anon', anonymous: true }, a = { key: 'a', anonymous: false }, b = { key: 'b', anonymous: false };
    expect(principalBarrierBlocks(undefined, a, false)).toBe(false);
    expect(principalBarrierBlocks(undefined, a, false, true)).toBe(true);
    expect(principalBarrierBlocks(anon, a, false)).toBe(false);
    expect(principalBarrierBlocks(a, a, false)).toBe(false);
    expect(principalBarrierBlocks(a, b, false)).toBe(true);
    expect(principalBarrierBlocks(a, anon, false)).toBe(true);
    expect(principalBarrierBlocks(a, a, true)).toBe(true);
  });
});
