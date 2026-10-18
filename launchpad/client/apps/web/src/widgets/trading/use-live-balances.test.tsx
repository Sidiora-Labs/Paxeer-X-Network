import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import React, { act } from 'react';
import type { Root } from 'react-dom/client';
import { renderToStaticMarkup } from 'react-dom/server';
import { afterAll, describe, expect, it, vi } from 'vitest';
import { createPublicClient, defineChain, erc20Abi, http, type Address, type PublicClient } from 'viem';

// vitest.config.ts sets `deps.interopDefault: false`; re-expose next/image's real default (see src/ui/shared/token-image.test.tsx).
vi.mock('next/image', async (importOriginal) => {
  const mod = await importOriginal<{ default: unknown }>();
  const cjs = mod.default as { default?: unknown } | undefined;
  return { ...mod, default: cjs && typeof cjs === 'object' && 'default' in cjs ? cjs.default : mod.default };
});

interface BrowserDom { window: Window & typeof globalThis; }
type JsdomModule = { JSDOM: new (html: string, options: { url: string; pretendToBeVisual: boolean }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: 'http://localhost:3000', pretendToBeVisual: true });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
for (const key of Object.getOwnPropertyNames(dom.window)) {
  if (key in globalThis || key.startsWith('_')) continue;
  const value = (dom.window as unknown as Record<string, unknown>)[key];
  browserGlobals[key] = typeof value === 'function' && !/^[A-Z]/.test(key) ? value.bind(dom.window) : value;
}
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;
browserGlobals.IS_REACT_ACT_ENVIRONMENT = true;

const { createRoot } = await import('react-dom/client');
const { authenticatedFor, useLiveBalances } = await import('./use-live-balances');
const { BalanceRow } = await import('./trade-panel-parts');
const { ChainTradeProvider, productionTradeDeployment } = await import('./transaction-controls/providers');

const UPSTREAM = 'https://api5.mainnet-beta.paxeer.network';
const USDL = '0x85fcd13735f4309833a503ee804ea32395851479' as Address;
// Deployer of the 2026-10-13 live test; any funded holder works.
const WALLET = '0x1DAe78b53C9F39930a2Ff2B6B7AaC12EC75FB154' as Address;
const chain = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: [UPSTREAM] } } });

/** Real JSON-RPC relay to chain 125 that counts calls and can be switched to answer 503. */
const relay = { calls: 0, failing: false };
const server: Server = createServer((request, response) => {
  const chunks: Buffer[] = [];
  request.on('data', chunk => chunks.push(chunk as Buffer));
  request.on('end', () => {
    relay.calls++;
    if (relay.failing) { response.statusCode = 503; response.end('upstream maintenance'); return; }
    void fetch(UPSTREAM, { method: 'POST', headers: { 'content-type': 'application/json' }, body: Buffer.concat(chunks) })
      .then(async upstream => { response.statusCode = upstream.status; response.setHeader('content-type', 'application/json'); response.end(await upstream.text()); })
      .catch(() => { response.statusCode = 502; response.end(); });
  });
});
await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
const relayUrl = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
const viaRelay = createPublicClient({ chain, transport: http(relayUrl, { retryCount: 0 }), cacheTime: 0 }) as PublicClient;
const direct = createPublicClient({ chain, transport: http(UPSTREAM, { retryCount: 1 }), cacheTime: 0 });
const provider = new ChainTradeProvider(viaRelay, undefined, productionTradeDeployment);

type Snapshot = ReturnType<typeof useLiveBalances>;
let latest: Snapshot | null = null;
function Probe(props: { enabled: boolean; intervalMs: number }) {
  latest = useLiveBalances({ reader: provider, usdl: USDL, token: USDL, account: WALLET, enabled: props.enabled, intervalMs: props.intervalMs });
  return null;
}

async function waitFor(check: () => boolean, timeoutMs = 20000): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('Condition not met in time');
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 25)); });
  }
}

describe('live wallet balances read from chain 125', () => {
  afterAll(async () => { await new Promise<void>(resolve => server.close(() => resolve())); dom.window.close(); });

  it('reads nothing while signed out, reads balanceOf live once signed in, re-polls, reports a failed read in full and drops values on sign-out', async () => {
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root: Root = createRoot(container);
    try {
      act(() => { root.render(<Probe enabled={false} intervalMs={300} />); });
      await act(async () => { await new Promise(resolve => setTimeout(resolve, 400)); });
      expect(relay.calls).toBe(0);
      expect(latest).toMatchObject({ usdl: null, token: null, observedAt: undefined, error: undefined });

      act(() => { root.render(<Probe enabled intervalMs={300} />); });
      await waitFor(() => latest?.usdl !== null && latest?.usdl !== undefined);
      const onChain = await direct.readContract({ address: USDL, abi: erc20Abi, functionName: 'balanceOf', args: [WALLET] });
      // Same chain read; a trade landing between the two reads is the only way they differ.
      expect(latest!.usdl).toBe(onChain);
      expect(latest!.error).toBeUndefined();
      expect(typeof latest!.observedAt).toBe('number');
      const first = latest!.observedAt!;
      await waitFor(() => (latest?.observedAt ?? 0) > first);
      expect(relay.calls).toBeGreaterThanOrEqual(4);

      const live = renderToStaticMarkup(<BalanceRow side="buy" usdlBalance={latest!.usdl} tokenBalance={latest!.token} tokenDecimals={6} tokenSymbol="USDL" error={latest!.error} onMax={() => {}} />);
      expect(live).not.toContain('stale');
      expect(live).not.toContain('truncate');

      relay.failing = true;
      await waitFor(() => latest?.error !== undefined);
      const held = latest!.usdl;
      expect(held).toBe(onChain);
      expect(latest!.error).toMatch(/HTTP request failed/);
      expect(latest!.error!.endsWith('…')).toBe(false);
      const failed = renderToStaticMarkup(<BalanceRow side="buy" usdlBalance={held} tokenBalance={latest!.token} tokenDecimals={6} tokenSymbol="USDL" error={latest!.error} onMax={() => {}} />);
      expect(failed).toContain('data-balance-stale');
      expect(failed).not.toContain('truncate');

      relay.failing = false;
      await waitFor(() => latest?.error === undefined);

      act(() => { root.render(<Probe enabled={false} intervalMs={300} />); });
      expect(latest).toMatchObject({ usdl: null, token: null, observedAt: undefined, error: undefined });
      const calls = relay.calls;
      await act(async () => { await new Promise(resolve => setTimeout(resolve, 700)); });
      expect(relay.calls).toBe(calls);
    } finally {
      act(() => root.unmount());
      container.remove();
    }
  }, 60000);

  it('treats a connected wallet without an account session for that wallet as signed out', () => {
    expect(authenticatedFor(true, WALLET, null)).toBe(false);
    expect(authenticatedFor(true, WALLET, undefined)).toBe(false);
    expect(authenticatedFor(false, WALLET, WALLET.toLowerCase())).toBe(false);
    expect(authenticatedFor(true, WALLET, '0x0000000000000000000000000000000000000001')).toBe(false);
    expect(authenticatedFor(true, null, WALLET)).toBe(false);
    expect(authenticatedFor(true, WALLET, WALLET.toLowerCase())).toBe(true);
  });

  it('shows no stale marker for a balance that has not failed, whatever its age', () => {
    const html = renderToStaticMarkup(<BalanceRow side="buy" usdlBalance={27909940078n} tokenBalance={null} tokenDecimals={6} tokenSymbol="USDL" onMax={() => {}} />);
    expect(html).toContain('27,909.94 USDL');
    expect(html).not.toContain('stale');
    const reading = renderToStaticMarkup(<BalanceRow side="buy" usdlBalance={null} tokenBalance={null} tokenDecimals={6} tokenSymbol="USDL" onMax={() => {}} />);
    expect(reading).toContain('Loading...');
    expect(reading).not.toContain('stale');
  });
});
