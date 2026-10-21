import { describe, it, expect, beforeAll, afterAll, beforeEach } from 'vitest';
import { createServer, type Server, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { readFileSync } from 'node:fs';
import { CHART_UDF_ROUTES, chartLibraryConfig, chartSymbolInfo, createReplayUdfDatafeed, canonicalCandleToDisplayBar, type ValuationState } from './udf-datafeed';
import { fetchCandleHistory, fetchCandleStats } from '@/core/clients/candle-stats';
import type { LibrarySymbolInfo } from '../../../public/charting_library/charting_library';

/**
 * charts-wire.json is the exact response bytes of core/trading-charts'
 * production registration (httpapi.RegisterProductionUDF: udfConfig,
 * udfTime, SymbolsHandler, ProductionHistoryHandler), recorded with
 * `go test -overlay` and a scratch test only; its history upstream was the
 * recorded core/api candle page in core-api-wire.json. The node server below
 * replays those bytes and the recorded core/api pages; it answers 404 for any
 * request that was not recorded.
 */
type Recorded = { path: string; status: number; headers: Record<string, string>; bodyBytes: string };
type CoreRecorded = { path: string; status: number; headers: Record<string, string>; body: unknown };
const fixtures = new URL('../../core/test-fixtures/market-production/', import.meta.url);
const charts = JSON.parse(readFileSync(new URL('charts-wire.json', fixtures), 'utf8')) as Record<'config' | 'time' | 'symbols' | 'unknownSymbol' | 'history' | 'valuationHistory' | 'statsDaily' | 'statsHourly' | 'statsFiveMinute', Recorded> & { now: number };
const core = JSON.parse(readFileSync(new URL('core-api-wire.json', fixtures), 'utf8')) as Record<'candles' | 'trades', CoreRecorded>;
const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
const unknownPool = `0x${'0'.repeat(40)}`;
const recordedCharts = [charts.config, charts.time, charts.symbols, charts.unknownSymbol, charts.history, charts.valuationHistory, charts.statsDaily, charts.statsHourly, charts.statsFiveMinute];

const sortedParams = (url: URL) => [...url.searchParams.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([key, value]) => `${key}=${value}`).join('&');
const sameRequest = (actual: URL, recordedPath: string) => {
  const recorded = new URL(recordedPath, 'http://recorded.invalid');
  return actual.pathname === recorded.pathname && sortedParams(actual) === sortedParams(recorded);
};

let server: Server;
let origin = '';
const requests: string[] = [];
/** Paths whose answer is held until released (or the client aborts). */
const held = new Set<string>();
const parked: Array<{ response: ServerResponse; release: () => void }> = [];
let aborted = 0;

beforeAll(async () => {
  server = createServer((request, response) => {
    const url = new URL(request.url ?? '/', 'http://fixture.invalid');
    requests.push(`${url.pathname}${url.search}`);
    const answer = () => {
      const chart = recordedCharts.find(entry => sameRequest(url, entry.path));
      if (chart) { response.writeHead(chart.status, chart.headers); response.end(chart.bodyBytes); return; }
      const api = [core.candles, core.trades].find(entry => sameRequest(url, entry.path));
      if (api) { response.writeHead(api.status, api.headers); response.end(JSON.stringify(api.body)); return; }
      response.writeHead(404, { 'Content-Type': 'application/json' }); response.end('{}');
    };
    if (!held.has(url.pathname)) { answer(); return; }
    response.on('close', () => { if (!response.writableEnded) aborted++; });
    parked.push({ response, release: answer });
  });
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
  origin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
});
afterAll(async () => { await new Promise<void>(resolve => server.close(() => resolve())); });
beforeEach(() => { requests.length = 0; held.clear(); parked.length = 0; aborted = 0; });

const windowOf = (recordedPath: string) => { const url = new URL(recordedPath, 'http://recorded.invalid'); return { from: Number(url.searchParams.get('from')), to: Number(url.searchParams.get('to')) }; };
async function until(check: () => boolean) {
  const deadline = Date.now() + 4000;
  while (!check()) { if (Date.now() > deadline) throw new Error('Condition not reached'); await new Promise(resolve => setTimeout(resolve, 10)); }
}

describe('core/trading-charts v2 UDF documents through the chart datafeed', () => {
  it('reads config, symbol and server time only from /v2/udf on the charts origin', async () => {
    const feed = createReplayUdfDatafeed(pool, 'price', () => {}, undefined, { apiOrigin: origin, valuationOrigin: origin });
    try {
      const config = await new Promise<Record<string, unknown>>(resolve => feed.onReady(resolve));
      expect(config).toEqual({ supported_resolutions: ['1', '5', '15', '60', '240', '1D', '1W'], supports_marks: true, supports_timescale_marks: false, supports_time: true, exchanges: [], symbols_types: [] });
      const info = await new Promise<LibrarySymbolInfo | string>(resolve => feed.resolveSymbol(pool, resolve, resolve));
      expect(info).toMatchObject({ name: pool, ticker: pool, description: 'LiveTest549002', type: 'token', exchange: 'sidiora', timezone: 'Etc/UTC', pricescale: 100000000, minmov: 1, currency_code: 'USDL', has_intraday: true, data_status: 'streaming' });
      const time = await new Promise<number>(resolve => feed.getServerTime(resolve));
      expect(time).toBe(Number(charts.time.bodyBytes));
      expect(requests).toEqual([CHART_UDF_ROUTES.config, `${CHART_UDF_ROUTES.symbols}?symbol=${pool}`, CHART_UDF_ROUTES.time]);
    } finally { feed.dispose(); }
  });

  it('reports an unknown pool with the production 404 and never resolves it', async () => {
    const feed = createReplayUdfDatafeed(unknownPool, 'price', () => {}, undefined, { apiOrigin: origin, valuationOrigin: origin });
    try {
      const outcome = await new Promise<{ resolved: unknown } | { error: string }>(resolve => feed.resolveSymbol(unknownPool, resolved => resolve({ resolved }), error => resolve({ error })));
      expect(outcome).toEqual({ error: 'Not available yet' });
      expect(charts.unknownSymbol.status).toBe(404);
    } finally { feed.dispose(); }
  });

  it('refuses a symbol document for another pool and keeps the market-cap scale', () => {
    const body: unknown = JSON.parse(charts.symbols.bodyBytes);
    expect(() => chartSymbolInfo(body, unknownPool, 'price')).toThrow('Chart symbol does not describe this pool');
    expect(chartSymbolInfo(body, pool.toUpperCase().replace('0X', '0x'), 'mcap')).toMatchObject({ pricescale: 100, description: 'LiveTest549002 (MCap)' });
    expect(() => chartLibraryConfig({ ...JSON.parse(charts.config.bodyBytes), supports_time: false })).toThrow();
  });

  it('charts market cap from the production total-supply valuation history', async () => {
    const states: ValuationState[] = [];
    const feed = createReplayUdfDatafeed(pool, 'mcap', () => {}, undefined, { apiOrigin: origin, valuationOrigin: origin }, () => {}, state => states.push(state));
    try {
      const { from, to } = windowOf(charts.valuationHistory.path);
      const outcome = await new Promise<{ bars: Array<{ time: number; open: number; close: number }> } | { error: string }>(resolve => feed.getBars({}, '1', { from, to }, bars => resolve({ bars }), error => resolve({ error })));
      const recorded = JSON.parse(charts.valuationHistory.bodyBytes) as { t: number[]; o: number[]; c: number[] };
      expect(outcome).toEqual({ bars: [expect.objectContaining({ time: recorded.t[0] * 1000 })] });
      if (!('bars' in outcome)) throw new Error('no bars');
      expect(outcome.bars[0].open).toBeCloseTo(recorded.o[0], 6);
      expect(outcome.bars[0].close).toBeCloseTo(recorded.c[0], 6);
      expect(states).toEqual([{ state: 'ready' }]);
      expect(requests.some(path => path.startsWith(`${CHART_UDF_ROUTES.history}?`) && path.includes('mode=total_supply_valuation'))).toBe(true);
    } finally { feed.dispose(); }
  });

  it('aborts in-flight chart documents on dispose and suppresses every callback', async () => {
    for (const path of Object.values(CHART_UDF_ROUTES)) held.add(path);
    const feed = createReplayUdfDatafeed(pool, 'price', () => {}, undefined, { apiOrigin: origin, valuationOrigin: origin });
    const calls: unknown[] = [];
    feed.onReady(value => calls.push(value));
    feed.resolveSymbol(pool, value => calls.push(value), error => calls.push(error));
    feed.getServerTime(value => calls.push(value));
    await until(() => parked.length === 3);
    expect(feed.resourceCounts().requests).toBe(3);
    feed.dispose();
    await until(() => aborted === 3);
    parked.forEach(entry => { if (!entry.response.destroyed) entry.release(); });
    await new Promise(resolve => setTimeout(resolve, 30));
    expect(calls).toEqual([]);
  });
});

describe('candle stats over the v2 history route', () => {
  it('parses the production price history for the recorded window', async () => {
    const { from, to } = windowOf(charts.history.path);
    const history = await fetchCandleHistory(pool, '1', from, to, { chartsOrigin: origin });
    const recorded = JSON.parse(charts.history.bodyBytes) as { t: number[]; o: number[]; h: number[]; l: number[]; c: number[]; v: number[] };
    expect(history).toEqual({ t: recorded.t, o: recorded.o, h: recorded.h, l: recorded.l, c: recorded.c, v: recorded.v });
    // The charts vendor numbers are core/api's exact candle ratios.
    const bar = canonicalCandleToDisplayBar((core.candles.body as { data: { items: unknown[] } }).data.items[0]);
    expect(history?.o[0]).toBeCloseTo(bar.open, 15);
    expect(history?.c[0]).toBeCloseTo(bar.close, 15);
    expect(history?.v[0]).toBe(bar.volume);
  });

  it('reads the production 503 for windows the canonical store cannot serve as no history', async () => {
    const stats = await fetchCandleStats(pool, { chartsOrigin: origin, now: charts.now });
    expect(stats).toEqual({ change24h: 0, change1h: 0, change5m: 0, ath: 0, atl: 0 });
    expect([...requests].sort()).toEqual([charts.statsDaily.path, charts.statsHourly.path, charts.statsFiveMinute.path].sort());
    expect([charts.statsDaily.status, charts.statsHourly.status, charts.statsFiveMinute.status]).toEqual([503, 503, 503]);
  });
});
