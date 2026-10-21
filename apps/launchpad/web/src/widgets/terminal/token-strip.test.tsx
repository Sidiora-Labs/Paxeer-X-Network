import React from 'react';
import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SdkClient } from '@kindle/sdk';
import { WagmiProvider, createConfig, http } from 'wagmi';
import { defineChain } from 'viem';
import { MarketClientContext } from '@/core/market/client';
import { queryKeys } from '@/core/query-keys';
import type { PoolSnapshot, PoolStats, Token } from '@/core/market/pool-resolver';
import TerminalTokenStrip, { tokenStripStats } from './token-strip';
import { clockLabel } from './chart-card';
import { TerminalLiveProvider } from './live-context';

const pool = '0x' + 'ab'.repeat(20);
const tokenAddress = '0x' + 'cd'.repeat(20);
const now = Date.parse('2026-10-12T15:00:00.000Z');
const BORDER = /\b(border(?!-0\b|-none\b)(-[a-z0-9/[\]-]+)?|divide-[a-z]|ring-(?!offset)[a-z0-9]|outline-(?!none|focus|2|offset)[a-z]+)\b/;

const paxeer = defineChain({ id: 125, name: 'Paxeer', nativeCurrency: { name: 'PAX', symbol: 'PAX', decimals: 18 }, rpcUrls: { default: { http: ['http://127.0.0.1:1'] } } });
const wagmiConfig = createConfig({ chains: [paxeer], transports: { [paxeer.id]: http() }, ssr: true, multiInjectedProviderDiscovery: false });

const watermark: PoolStats['watermark'] = { blockHash: '0x' + '1'.repeat(64), chainId: '125', height: '100', integrity: 'healthy', observedAt: '2026-10-12T14:59:00.000Z', projectionRevision: '7' };
const stats: PoolStats = {
  poolAddress: pool,
  buys24h: '12',
  sells24h: '8',
  changeBaselineQuality: 'complete',
  circulatingMarketCapUsdlRaw: '3120000000',
  totalSupplyValuationUsdlRaw: '3120000000',
  holderCount: '3',
  indicativeTvlUsdlRaw: null,
  priceChangeBps24h: '8',
  realUsdlBalanceRaw: '6420000000',
  sourceUpperPosition: { blockHash: '0x' + '2'.repeat(64), chainId: '125', height: '100', logIndex: '0', transactionHash: '0x' + '3'.repeat(64), transactionIndex: '0' },
  supplyValuationQuality: 'verified',
  valuationChangeUsdlRaw: null,
  valuationMethod: null,
  volume24hRaw: '1300000000',
  watermark,
  windowEnd: '2026-10-12T15:00:00.000Z',
  windowStart: '2026-10-11T15:00:00.000Z',
};
const token: Token = {
  address: tokenAddress,
  decimals: 18,
  decimalsQuality: 'verified',
  logoAssetId: null,
  metadataRevision: '1',
  name: 'Doge Troll',
  sourcePosition: null,
  supplyQuality: 'verified',
  symbol: 'DOLL',
  totalSupplyRaw: '1000000000' + '0'.repeat(18),
};
const byKey = (input: Parameters<typeof tokenStripStats>[0]) => Object.fromEntries(tokenStripStats(input).map(stat => [stat.key, stat]));

describe('tokenStripStats', () => {
  it('maps liquidity from realUsdlBalanceRaw and supply from the verified token supply', () => {
    const tiles = byKey({ stats, token, livePrice: 0.0000031245 });
    expect(tiles.liquidity).toMatchObject({ label: 'Liquidity', value: '6.42K', unit: 'USDL', reason: null });
    expect(tiles.supply).toMatchObject({ label: 'Supply', value: '1B', reason: null });
    expect(tiles.price).toMatchObject({ value: '$0.0₅3124', reason: null });
    expect(tokenStripStats({ stats, token, livePrice: null }).map(stat => stat.label)).toEqual(['All time high', 'Price', 'Liquidity', 'Supply', 'Total fees paid', 'Bonding curve']);
  });

  it('keeps every unserved stat empty with a plain reason and no operation ids', () => {
    const tiles = byKey({ stats, token, livePrice: null });
    expect(tiles.ath.value).toBeNull();
    expect(tiles.ath.reason).toBe('All-time high is not available yet');
    expect(tiles.fees.value).toBeNull();
    expect(tiles.fees.reason).toBe('Total fees paid is not available yet');
    for (const tile of Object.values(tiles)) expect(tile.reason ?? '').not.toMatch(/IF_|GET \/|\{p\}/);
    expect(tiles.curve.value).toBeNull();
    expect(tiles.curve.reason).toMatch(/Bonding curve/);
  });

  it('falls back to the served v2 spotPriceWad and says so, and dashes the price when nothing is served', () => {
    expect(byKey({ stats: { ...stats, spotPriceWad: '10098226981692' }, token, livePrice: null }).price).toMatchObject({ value: '$0.0₄1009', reason: expect.stringMatching(/Last known price/) });
    expect(byKey({ stats: { ...stats, spotPriceWad: null }, token, livePrice: null }).price).toMatchObject({ value: null, reason: expect.stringMatching(/first trade/) });
    expect(byKey({ stats, token, livePrice: null }).price).toMatchObject({ value: null, reason: expect.stringMatching(/first trade/) });
  });

  it('dashes liquidity and supply with the reason when the reads are missing or unverified', () => {
    const missing = byKey({ stats: null, token: null, livePrice: null });
    expect(missing.liquidity).toMatchObject({ value: null, reason: 'Liquidity is not available for this coin yet' });
    expect(missing.supply).toMatchObject({ value: null, reason: 'Coin details are not available yet' });
    expect(byKey({ stats, token: { ...token, decimals: null, decimalsQuality: 'unavailable' }, livePrice: null }).supply).toMatchObject({ value: null, reason: 'Supply is not available yet' });
    expect(byKey({ stats, token: { ...token, totalSupplyRaw: null, supplyQuality: 'unavailable' }, livePrice: null }).supply).toMatchObject({ value: null, reason: 'Supply is not available yet' });
    expect(byKey({ stats, token: { ...token, supplyQuality: 'partial' }, livePrice: null }).supply).toMatchObject({ value: '1B', reason: 'Supply may not be final yet' });
  });
});

describe('TerminalTokenStrip', () => {
  function render(snapshot: PoolSnapshot): string {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    client.setQueryData(queryKeys.poolSnapshot(pool), snapshot);
    const sdk = new SdkClient({ apiOrigin: 'http://127.0.0.1:1', identityOrigin: 'http://127.0.0.1:1', mediaOrigin: 'http://127.0.0.1:1', streamOrigin: 'ws://127.0.0.1:1', approvedStreamOrigins: ['ws://127.0.0.1:1'], production: false });
    return renderToStaticMarkup(
      <WagmiProvider config={wagmiConfig}>
        <QueryClientProvider client={client}>
          <MarketClientContext.Provider value={sdk}><TerminalLiveProvider poolAddress={pool}><TerminalTokenStrip poolAddress={pool} /></TerminalLiveProvider></MarketClientContext.Provider>
        </QueryClientProvider>
      </WagmiProvider>,
    );
  }

  const snapshot: PoolSnapshot = {
    pool: { chainId: '125', poolAddress: pool, tokenAddress },
    token,
    metadata: null,
    stats,
    creator: null,
    createdAt: new Date(now - 22 * 3600 * 1000).toISOString(),
    generatedAt: new Date(now).toISOString(),
    resolvedBy: 'detail',
    unavailable: { stats: null, metadata: null, registry: null },
  };

  it('renders the identity, the served tiles and labelled actions without border strokes', () => {
    const html = render(snapshot);
    expect(html).toContain('DOLL');
    expect(html).toContain('Doge Troll');
    expect(html).toContain('6.42K');
    expect(html).toContain('1B');
    expect(html).toContain('aria-label="Price alerts"');
    expect(html).toContain('aria-label="Terminal settings"');
    expect(html).toContain('aria-label="Add to watchlist"');
    expect(html.match(/>-</g)?.length).toBeGreaterThanOrEqual(3);
    expect(html).not.toMatch(BORDER);
  });
});

describe('clockLabel', () => {
  it('renders HH:MM:SS with the UTC offset', () => {
    expect(clockLabel(now)).toMatch(/^\d{2}:\d{2}:\d{2} \(UTC[+-]\d{1,2}(:\d{2})?\)$/);
  });
});
