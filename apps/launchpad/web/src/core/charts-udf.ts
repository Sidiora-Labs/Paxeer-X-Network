/**
 * core/trading-charts' versioned UDF surface (RegisterProductionUDF). The
 * legacy unversioned UDF routes on core/api and the legacy candle sockets
 * (charts and core/api) are not used: chart bars come from core/api
 * `/v2/pools/{pool}/candles`, live candles from core/api's `/v2/stream`
 * `market.candles.{pool}.{interval}` topics, and these documents from charts.
 */
export const CHART_UDF_ROUTES = { config: '/v2/udf/config', symbols: '/v2/udf/symbols', history: '/v2/udf/history', time: '/v2/udf/time' } as const;

/**
 * Origin serving CHART_UDF_ROUTES: the explicit charts authority, else the
 * page origin, which next.config.ts rewrites to core/trading-charts
 * (WEB_CHARTS_UPSTREAM). core/api does not serve these routes.
 */
export function chartsOrigin(explicit?: string): string {
  const origin = explicit ?? globalThis.location?.origin;
  if (!origin || origin === 'null') throw new Error('Trading charts origin unavailable');
  return origin;
}
