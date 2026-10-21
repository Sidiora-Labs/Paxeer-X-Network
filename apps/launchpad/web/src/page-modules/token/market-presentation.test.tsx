// @vitest-environment jsdom
import { describe, it, expect } from 'vitest';
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { MarketStats, MarketHolders } from './market-metrics';
import { MarketTradeFeed } from './market-trade-feed';
import { decodeContract } from '@kindle/sdk';
import { appendRankingPage, mergeTradeHistory, tradeKey } from './market-session';
import { safeSocialUrl } from '@/widgets/trading/token-description';
import { CoinCard } from '@/ui/primitives';
import { poolPath } from '@/core/app-foundation/navigation';
import { exactUnits, tokenAmount, freshness, dataAgeMs, orderedTrades, holderRows, rankingCards, priceNumber, type PoolStats, type Pool, type Token, type Trade, type HoldersResponse, type Watermark, type RankingsResponse } from './market-presentation';
Object.assign(globalThis, { IS_REACT_ACT_ENVIRONMENT: true });
const address = '0x' + '1'.repeat(40);
const position = { chainId: '125' as const, height: '20', transactionIndex: '1', logIndex: '0', transactionHash: '0x' + '2'.repeat(64), blockHash: '0x' + '3'.repeat(64) };
const watermark: Watermark = { chainId: '125', height: '20', blockHash: position.blockHash, projectionRevision: '1', integrity: 'healthy', observedAt: '2026-01-01T00:00:00.000Z' };
const token: Token = { address, decimals: 3, decimalsQuality: 'verified', totalSupplyRaw: '123456', supplyQuality: 'verified', name: '<script>bad()</script>', symbol: 'SAME', metadataRevision: '1', logoAssetId: null, sourcePosition: position };
const pool: Pool = { chainId: '125', poolAddress: address, tokenAddress: address, creator: address, quoteAssetAddress: address, quoteAssetDecimals: 6, feeBps: 1, createdPosition: position, realUsdlBalanceRaw: '1234567', virtualUsdlReserveRaw: '999999999', effectivePricingUsdlReserveRaw: '1001234566', tokenReserveRaw: '3' };
describe('schema-bound market primitives (synthetic arithmetic fixtures)', () => {
  it('formats actual decimals, huge integers and unavailable metadata without Number coercion', () => {
    expect(exactUnits('1234567', 6)).toBe('1.234567');
    expect(tokenAmount('123456', token)).toBe('123.456');
    expect(exactUnits('9'.repeat(78), 0)).toBe('9'.repeat(78));
    expect(exactUnits('1', 255)).toBe('≈0');
    expect(tokenAmount('1', { ...token, decimalsQuality: 'unavailable' })).toBe('—');
    expect(() => exactUnits('1e20', 6)).toThrow();
    expect(() => exactUnits('1', 256)).toThrow();
  });
  it('keeps the significant digits of small positive prices when converting ratios', () => {
    expect(priceNumber({ numerator: '1', denominator: '10000000' })).toBe(1e-7);
    expect(priceNumber({ numerator: '123', denominator: '1000000000000' })).toBe(1.23e-10);
    expect(priceNumber({ numerator: '1', denominator: '3' })).toBeCloseTo(1 / 3, 15);
    expect(priceNumber({ numerator: '1250000', denominator: '1000000' })).toBe(1.25);
    expect(priceNumber({ numerator: '9'.repeat(30), denominator: '1' })).toBe(1e30);
    expect(priceNumber({ numerator: '0', denominator: '7' })).toBe(0);
    expect(priceNumber({ numerator: '1', denominator: '0' })).toBeNull();
    expect(priceNumber({ numerator: '1' + '0'.repeat(400), denominator: '1' })).toBeNull();
  });
  it('retains stale and gap status until snapshot continuity is confirmed', () => {
    const base = { now: 100000, generatedAt: new Date(100000).toISOString(), heartbeatAt: 100000, contiguous: true, watermark: { ...watermark, observedAt: new Date(100000).toISOString() } };
    expect(freshness(base)).toBe('current');
    expect(freshness({ ...base, heartbeatAt: NaN })).toBe('reconnecting');
    expect(freshness({ ...base, heartbeatAt: 106000 })).toBe('delayed');
    expect(freshness({ ...base, contiguous: false })).toBe('reconnecting');
    expect(freshness({ ...base, heartbeatAt: 89999 })).toBe('reconnecting');
    expect(freshness({ ...base, generatedAt: new Date(106000).toISOString() })).toBe('delayed');
    expect(freshness({ ...base, watermark: { ...watermark, integrity: 'quarantined' } })).toBe('stale');
  });
  it('uses the heartbeat for liveness and the watermark only for data age', () => {
    const base = { now: 100000, generatedAt: new Date(100000).toISOString(), heartbeatAt: 100000, contiguous: true, watermark: { ...watermark, observedAt: new Date(100000).toISOString() } };
    // An idle pool: the newest projection is minutes old but heartbeats keep arriving.
    const idle = { ...base, generatedAt: new Date(10000).toISOString(), watermark: { ...watermark, observedAt: new Date(5000).toISOString() } };
    expect(freshness(idle)).toBe('current');
    expect(freshness({ ...idle, heartbeatAt: 91000 })).toBe('current');
    expect(dataAgeMs(idle.now, idle.watermark)).toBe(95000);
    // Liveness lost: reconnecting first, stale once nothing alive was seen for 30 s.
    expect(freshness({ ...idle, heartbeatAt: 85000 })).toBe('reconnecting');
    expect(freshness({ ...idle, heartbeatAt: 69999 })).toBe('stale');
    expect(freshness({ ...idle, heartbeatAt: null })).toBe('stale');
    expect(freshness({ ...base, heartbeatAt: null })).toBe('reconnecting');
    expect(dataAgeMs(100000, null)).toBeNull();
  });
  it('orders and deduplicates chain events rather than arrival timestamps', () => {
    const a: Trade = { ...trade, position, eventId: 'a' };
    const b: Trade = { ...trade, position: { ...position, logIndex: '2' }, eventId: 'b' };
    expect(orderedTrades([a, b, a]).map(x => x.eventId)).toEqual(['b', 'a']);
  });
  it('sorts holders numerically and rejects mixed ranking snapshots', () => {
    const response: HoldersResponse = { data: { coverage: 'partial', excludedProtocolAddresses: [], exclusionPolicyVersion: '1', fromPosition: position, toPosition: position, items: [{ wallet: address, tokenAddress: address, balanceRaw: '9', share: null }, { wallet: '0x' + '4'.repeat(40), tokenAddress: address, balanceRaw: '100', share: null }] }, generatedAt: watermark.observedAt, nextCursor: null, quality: 'partial', requestId: 'holder-order', revision: '1', watermark };
    expect(holderRows(response)[0].balanceRaw).toBe('100');
    const ranking: RankingsResponse = { data: { snapshot: { category: 'new', modelVersion: '1', computedAt: watermark.observedAt, snapshotId: 'ranking-1', replace: true, totalEligible: '1', truncated: false, watermark, items: [{ category: 'movers', modelVersion: '1', computedAt: watermark.observedAt, poolAddress: address, rank: 1, score: '1' }] } }, generatedAt: watermark.observedAt, nextCursor: null, quality: 'complete', requestId: 'ranking-order', revision: '1', watermark };
    expect(() => rankingCards(ranking)).toThrow('Incompatible');
  });
});

const stats: PoolStats = { poolAddress: address, buys24h: '9', sells24h: '100', changeBaselineQuality: 'insufficient_history', circulatingMarketCapUsdlRaw: null, holderCount: null, indicativeTvlUsdlRaw: null, priceChangeBps24h: null, realUsdlBalanceRaw: '1234567', sourceUpperPosition: position, supplyValuationQuality: 'unavailable', totalSupplyValuationUsdlRaw: null, valuationChangeUsdlRaw: null, valuationMethod: null, volume24hRaw: '999999999999999999999999999999999999999999999999999999999999999999999999999999', watermark, windowStart: '2025-12-31T00:00:00.000Z', windowEnd: '2026-01-01T00:00:00.000Z' };
const trade: Trade = { ammInputAfterFeeRaw: null, codeEpoch: '1', emittingContract: address, eventId: 'event-1', feeAssetAddress: address, feeAssetDecimals: 6, feeDisposition: 'distributed_usdl', feeRaw: '0', inputDerivationQuality: 'legacy_unavailable', paidInputRaw: null, poolAddress: address, position, pricedInputRaw: null, recipient: null, recipientAttribution: 'unavailable', recipientEvidenceRefs: [], sender: address, side: 'buy', tokenAmountRaw: '2000', usdlAmountRaw: '3000000' };
describe('schema-bound metric and fill presentation', () => {
  it('renders exact USDL values, qualified windows and missing baselines without invented zero', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    await act(async () => root.render(<MarketStats poolAddress={address} stats={stats} />));
    expect(host.textContent).toContain('Volume · USDL · 24h');
    expect(host.textContent).toContain('999999999999999999999999999999999999999999999999999999999999999999999999.999999');
    expect(host.textContent).toContain('Price change · 24h—');
    expect(host.textContent).toContain('height 20 · revision 1');
    await act(async () => root.render(<MarketStats poolAddress={address} stats={{ ...stats, changeBaselineQuality: 'complete', priceChangeBps24h: '-125' }} />));
    expect(host.textContent).toContain('-1.25%');
    await act(async () => root.render(<MarketStats poolAddress={address} stats={null} />));
    expect(host.textContent).toContain('Market statistics unavailable');
    await act(async () => root.unmount()); host.remove();
  });
  it('sorts actual balances numerically and preserves partial coverage without guessing supply denominator', async () => {
    const holderAddress = '0x' + '4'.repeat(40);
    const response: HoldersResponse = { data: { coverage: 'partial', excludedProtocolAddresses: [], exclusionPolicyVersion: '1', fromPosition: position, toPosition: position, items: [{ wallet: address, tokenAddress: address, balanceRaw: '9', share: { numerator: '9', denominator: '109' } }, { wallet: holderAddress, tokenAddress: address, balanceRaw: '100', share: { numerator: '100', denominator: '109' } }] }, generatedAt: watermark.observedAt, nextCursor: null, quality: 'partial', requestId: 'holders-test', revision: '1', watermark };
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    await act(async () => root.render(<MarketHolders response={response} token={token} />));
    expect(host.querySelector('tbody tr')?.textContent).toContain(holderAddress);
    expect(host.querySelector('tbody tr')?.textContent).toContain('0.1');
    expect(host.textContent).toContain('Coverage: partial');
    expect(host.textContent).toContain('concentration percentages unavailable');
    expect(host.textContent).toContain('Incomplete coverage');
    await act(async () => root.unmount()); host.remove();
  });
  it('renders a pool with no fills as an empty state, with or without a watermark', async () => {
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    for (const mark of [watermark, null]) {
      await act(async () => root.render(<MarketTradeFeed pool={pool} token={token} rows={[]} watermark={mark} contiguous={false} />));
      expect(host.textContent).toContain('No trades yet');
      expect(host.textContent).toContain('Trades appear here as soon as someone buys or sells this coin.');
      expect(host.querySelector('[role="alert"]')).toBeNull();
      expect(host.textContent).not.toMatch(/unavailable|replay boundary/i);
    }
    await act(async () => root.unmount()); host.remove();
  });
  it('renders deduplicated chain order, actual token decimals and execution price with explicit attribution', async () => {
    const second: Trade = { ...trade, eventId: 'event-2', position: { ...position, logIndex: '2' }, side: 'sell' };
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    await act(async () => root.render(<MarketTradeFeed pool={pool} token={token} rows={[trade, second, trade]} watermark={watermark} contiguous={false} explorerOrigin="https://example.org" />));
    expect(host.querySelectorAll('tbody tr')).toHaveLength(2);
    expect(host.querySelector('tbody tr')?.textContent).toContain('sell');
    expect(host.textContent).toContain('1.5');
        expect(host.textContent).toContain('Catching up');
    expect(host.textContent).not.toContain('unavailable');
    expect(host.querySelector('a')?.getAttribute('href')).toBe('https://example.org/tx/' + position.transactionHash);
    const buttons = host.querySelectorAll('button');
    await act(async () => buttons[1].click());
    expect(host.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(host.querySelector('tbody tr')?.textContent).toContain('buy');
    await act(async () => root.unmount()); host.remove();
  });
});

describe('adversarial market presentation through production contracts', () => {
  it('rejects malformed decimal payloads while preserving schema-valid huge amounts and missing supply', () => {
    const maximum = '115792089237316195423570985008687907853269984665640564039457584007913129639935';
    const accepted = decodeContract('Trade', { ...trade, tokenAmountRaw: maximum, usdlAmountRaw: maximum });
    expect(exactUnits(accepted.usdlAmountRaw, 6)).toContain(maximum.slice(0, -6));
    for (const invalid of ['1e20', '-1', '01', 'NaN', 'Infinity', '1.1', '<script>1</script>']) {
      expect(() => decodeContract('Trade', { ...trade, usdlAmountRaw: invalid })).toThrow();
      expect(() => orderedTrades([{ ...trade, usdlAmountRaw: invalid }])).toThrow();
    }
    expect(decodeContract('Token', { ...token, totalSupplyRaw: null, supplyQuality: 'unavailable' }).totalSupplyRaw).toBeNull();
    expect(exactUnits(null, 6)).toBe('—');
  });
  it('allows only explicitly configured HTTPS social origins and cannot redirect by credentials or suffix matching', () => {
    const origins = ['https://example.org'];
    expect(safeSocialUrl('https://example.org/profile?a=1', origins)).toBe('https://example.org/profile?a=1');
    for (const invalid of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', 'https://example.org.attacker.invalid/', 'https://example.org@attacker.invalid/', 'https://user:password@example.org/', 'http://example.org/']) expect(safeSocialUrl(invalid, origins)).toBeNull();
    expect(safeSocialUrl('https://example.org/', [])).toBeNull();
  });
  it('keeps duplicate symbols distinct by canonical identity and renders HTML-like metadata as text', async () => {
    const secondAddress = '0x' + '4'.repeat(40);
    const hostile = decodeContract('Token', { ...token, name: '<img src=x onerror=alert(1)>', symbol: 'SAME', totalSupplyRaw: null, supplyQuality: 'unavailable' });
    const other = decodeContract('Token', { ...hostile, address: secondAddress });
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    try {
      await act(async () => root.render(<>{[hostile, other].map(item => <CoinCard key={item.address} image={null} name={item.name} symbol={item.symbol} mcapRaw={null} percentChange={null} createdAt={null} href={poolPath(125, item.address)} />)}</>));
      expect(host.querySelector('script,img')).toBeNull();
      expect(host.textContent).toContain('<img src=x onerror=alert(1)>');
      const hrefs = [...host.querySelectorAll('a')].map(anchor => anchor.getAttribute('href'));
      expect(hrefs).toContain('/coin/' + address); expect(hrefs).toContain('/coin/' + secondAddress);
      expect(hrefs.every(href => href?.startsWith('/coin/'))).toBe(true);
    } finally { await act(async () => root.unmount()); host.remove(); }
  });
  it('rejects duplicate canonical ranking identities and enforces the cache bound', () => {
    const entry = { category: 'new' as const, modelVersion: '1', computedAt: watermark.observedAt, poolAddress: address, rank: 1, score: '1' };
    const response = decodeContract('RankingsResponse', { data: { snapshot: { category: 'new', modelVersion: '1', computedAt: watermark.observedAt, snapshotId: '6f1c2a4e-8b3d-4c5e-9a7f-1d2e3f4a5b6c', replace: true, totalEligible: '2', truncated: false, watermark, items: [entry, { ...entry, rank: 2 }] } }, generatedAt: watermark.observedAt, nextCursor: null, quality: 'complete', requestId: '0b9e8d7c-6a5f-4e3d-8c2b-1a0f9e8d7c6b', revision: '1', watermark });
    expect(() => appendRankingPage(null, response)).toThrow('Duplicate ranking');
    const rows = Array.from({ length: 1000 }, (_, index) => decodeContract('Trade', { ...trade, eventId: `bounded-${index}`, position: { ...position, logIndex: String(index) } }));
    const incoming = decodeContract('Trade', { ...trade, eventId: 'new-head', position: { ...position, height: '21', logIndex: '1000' } });
    const merged = mergeTradeHistory(rows, [incoming, incoming]);
    expect(merged).toHaveLength(1000); expect(tradeKey(merged[0])).toBe(tradeKey(incoming));
    expect(new Set(merged.map(tradeKey)).size).toBe(1000);
    expect(() => mergeTradeHistory([...rows, incoming], [])).toThrow('bound');
  });
  it('preserves a scrolled fill anchor when new head fills arrive and bounds rendered rows', async () => {
    const rows = Array.from({ length: 120 }, (_, index) => decodeContract('Trade', { ...trade, eventId: `anchor-${index}`, position: { ...position, logIndex: String(index) } }));
    const incoming = decodeContract('Trade', { ...trade, eventId: 'anchor-new', position: { ...position, height: '21', logIndex: '120' } });
    const host = document.createElement('div'); document.body.append(host); const root = createRoot(host);
    try {
      await act(async () => root.render(<MarketTradeFeed pool={pool} token={token} rows={rows} watermark={watermark} contiguous={false} />));
      const scroller = host.querySelector<HTMLDivElement>('div.overflow-auto');
      expect(scroller).not.toBeNull();
      await act(async () => { scroller!.scrollTop = 20 * 48 + 7; scroller!.dispatchEvent(new Event('scroll', { bubbles: true })); });
      const before = host.querySelector('tbody tr:not([aria-hidden])')?.textContent;
      await act(async () => root.render(<MarketTradeFeed pool={pool} token={token} rows={[incoming, ...rows]} watermark={watermark} contiguous={false} />));
      expect(scroller!.scrollTop).toBe(21 * 48 + 7);
      expect(host.querySelector('tbody tr:not([aria-hidden])')?.textContent).toBe(before);
      expect(host.textContent).toContain('Show 1 new trades');
      expect(host.textContent).toContain('Catching up');
      expect(host.querySelectorAll('tbody tr').length).toBeLessThanOrEqual(102);
    } finally { await act(async () => root.unmount()); host.remove(); }
  });
});
