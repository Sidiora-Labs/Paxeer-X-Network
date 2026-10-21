import { CHART_UDF_ROUTES, chartsOrigin } from '@/core/charts-udf';

export interface CandleStats {
  change24h: number;    // % change over 24h
  change1h: number;     // % change over 1h
  change5m: number;     // % change over 5m
  ath: number;          // all-time high price
  atl: number;          // all-time low price (excluding 0)
}

export interface CandleHistory { t: number[]; o: number[]; h: number[]; l: number[]; c: number[]; v: number[] }

export interface CandleStatsOptions {
  /** core/trading-charts origin; defaults to the page origin (next.config.ts rewrites /v2/udf/history). */
  chartsOrigin?: string;
  /** Epoch seconds the windows end at; defaults to now. */
  now?: number;
  signal?: AbortSignal;
}

const series = (value: unknown, length: number): value is number[] =>
  Array.isArray(value) && value.length === length && value.every(item => typeof item === 'number' && Number.isFinite(item));

/**
 * Price-mode bars from core/trading-charts GET /v2/udf/history
 * (ProductionHistoryHandler). `s: "no_data"` and every non-ok answer
 * (unavailable, 4xx/5xx error documents) read as no history.
 */
export async function fetchCandleHistory(poolAddress: string, resolution: string, fromTs: number, toTs: number, options: CandleStatsOptions = {}): Promise<CandleHistory | null> {
  try {
    const url = new URL(CHART_UDF_ROUTES.history, chartsOrigin(options.chartsOrigin));
    for (const [key, value] of Object.entries({ symbol: poolAddress.toLowerCase(), resolution, from: String(fromTs), to: String(toTs) })) url.searchParams.set(key, value);
    const res = await fetch(url, { signal: options.signal, headers: { Accept: 'application/json', 'X-Schema-Version': '2' } });
    if (!res.ok || res.headers.get('X-Schema-Version') !== '2') return null;
    const data: unknown = await res.json();
    if (!data || typeof data !== 'object' || Array.isArray(data)) return null;
    const body = data as Record<string, unknown>;
    if (body.s !== 'ok' || body.mode !== 'price' || !Array.isArray(body.t) || body.t.length === 0) return null;
    const length = body.t.length;
    if (!series(body.t, length) || !series(body.o, length) || !series(body.h, length) || !series(body.l, length) || !series(body.c, length) || !series(body.v, length)) return null;
    return { t: body.t, o: body.o, h: body.h, l: body.l, c: body.c, v: body.v };
  } catch {
    return null;
  }
}

function pctChange(oldPrice: number, newPrice: number): number {
  if (oldPrice <= 0) return 0;
  return ((newPrice - oldPrice) / oldPrice) * 100;
}

export async function fetchCandleStats(poolAddress: string, options: CandleStatsOptions = {}): Promise<CandleStats> {
  const now = options.now ?? Math.floor(Date.now() / 1000);

  const [daily, hourly, fiveMin] = await Promise.all([
    fetchCandleHistory(poolAddress, '1D', now - 365 * 86400, now, options),
    fetchCandleHistory(poolAddress, '60', now - 2 * 3600, now, options),
    fetchCandleHistory(poolAddress, '5', now - 600, now, options),
  ]);

  let ath = 0;
  let atl = 0;
  let change24h = 0;
  let change1h = 0;
  let change5m = 0;

  if (daily && daily.c.length > 0) {
    ath = Math.max(...daily.h);
    const positiveLows = daily.l.filter(v => v > 0);
    if (positiveLows.length > 0) atl = Math.min(...positiveLows);
    if (daily.c.length >= 2) change24h = pctChange(daily.c[daily.c.length - 2], daily.c[daily.c.length - 1]);
  }

  if (hourly && hourly.c.length >= 2) change1h = pctChange(hourly.c[0], hourly.c[hourly.c.length - 1]);

  if (fiveMin && fiveMin.c.length >= 2) change5m = pctChange(fiveMin.c[0], fiveMin.c[fiveMin.c.length - 1]);

  return { change24h, change1h, change5m, ath, atl };
}
