// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import { path as __scalarPath } from '../internal/utils/path';
import type * as PriceFeedsAPI from './price-feeds';
import type * as TradingViewAPI from './trading-view';

export class Markets extends APIResource {
  /**
   * Get aggregator health
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketsHealth>} Successful response.
   *
   * @example
   * ```ts
   * const marketsHealth = await client.markets.listHealth();
   * ```
   */
  listHealth(options?: RequestOptions): APIPromise<MarketsHealth> {
    return this._client.get('/api/markets/health', options);
  }

  /**
   * List display-scaled market summaries
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketListResponse>} Successful response.
   *
   * @example
   * ```ts
   * const market = await client.markets.list();
   * ```
   */
  list(options?: RequestOptions): APIPromise<MarketListResponse> {
    return this._client.get('/api/markets/markets', options);
  }

  /**
   * Get one display-scaled market summary
   *
   * @param {PriceFeedsAPI.Symbol} symbol_ - Uppercase market symbol; gateway path matching is case-sensitive.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketRetrieveResponse>} Successful response.
   *
   * @example
   * ```ts
   * const market = await client.markets.retrieve('BTC');
   * ```
   */
  retrieve(symbol_: PriceFeedsAPI.Symbol, options?: RequestOptions): APIPromise<MarketRetrieveResponse> {
    return this._client.get(__scalarPath`/api/markets/markets/${symbol_}`, options);
  }

  /**
   * Get display-scaled aggregate totals
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketListTotalsResponse>} Successful response.
   *
   * @example
   * ```ts
   * const market = await client.markets.listTotals();
   * ```
   */
  listTotals(options?: RequestOptions): APIPromise<MarketListTotalsResponse> {
    return this._client.get('/api/markets/totals', options);
  }

  /**
   * Omits markets without a positive price ingested during the last 30 seconds; data may be empty or contain fewer than 18 markets.
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketListTickersResponse>} Successful response.
   *
   * @example
   * ```ts
   * const market = await client.markets.listTickers();
   * ```
   */
  listTickers(options?: RequestOptions): APIPromise<MarketListTickersResponse> {
    return this._client.get('/api/markets/tickers', options);
  }

  /**
   * Unknown, missing or stale prices return HTTP 503, not 404.
   *
   * @param {string} symbol_ - Bare uppercase symbol (BTC) or wrapped symbol (PERP_BTC_USDC).
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketRetrieveTickerResponse>} Successful response.
   *
   * @example
   * ```ts
   * const market = await client.markets.retrieveTicker('symbol');
   * ```
   */
  retrieveTicker(symbol_: string, options?: RequestOptions): APIPromise<MarketRetrieveTickerResponse> {
    return this._client.get(__scalarPath`/api/markets/ticker/${symbol_}`, options);
  }

  /**
   * Get estimated long/short perpetual open interest
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<MarketListOpenInterestResponse>} Successful response.
   *
   * @example
   * ```ts
   * const market = await client.markets.listOpenInterest();
   * ```
   */
  listOpenInterest(options?: RequestOptions): APIPromise<MarketListOpenInterestResponse> {
    return this._client.get('/api/markets/open-interest', options);
  }
}

export interface MarketsHealth {
  ok: boolean;
  service: 'markets-aggregator';
  /**
   * @format int64
   */
  symbols: number;
  /**
   * @format int64
   */
  uptimeSec: number;
  ws: WsRuntimeStats;
  poller: MarketsHealth.Poller;
}

export namespace MarketsHealth {
  export interface Poller {
    /**
     * @format int64
     */
    pollCount: number;
    errorCounts: Record<string, number>;
  }
}

/**
 * Unix epoch milliseconds.
 */
export type UnixMilliseconds = number;

/**
 * Display view: volume_24h, open_interest, volume_7d, volume_30d use operator-configured display divisors; reported_volume_24h and reported_open_interest preserve unscaled service values.
 */
export interface MarketRecord {
  symbol: PriceFeedsAPI.Symbol;
  symbol_full: string;
  /**
   * @format double
   */
  price: number;
  /**
   * @format double
   */
  change_24h: number;
  /**
   * @format double
   */
  volume_24h: number;
  /**
   * @format double
   */
  open_interest: number;
  /**
   * @format double
   */
  funding_8h: number;
  /**
   * @format double
   */
  est_funding_rate: number;
  /**
   * @format double
   */
  volume_7d: number;
  /**
   * @format double
   */
  volume_30d: number;
  /**
   * @format double
   */
  trades_count: number;
  /**
   * @format double
   */
  reported_volume_24h: number;
  /**
   * @format double
   */
  reported_open_interest: number;
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  last_updated: UnixMilliseconds;
  /**
   * @format double
   */
  perp_mark_price?: number;
  /**
   * @format double
   */
  perp_index_price?: number;
  /**
   * @format double
   */
  perp_basis_bps?: number;
  /**
   * @format double
   */
  perp_funding_rate?: number;
  /**
   * @format double
   */
  perp_next_funding_ms?: number;
  /**
   * @format double
   */
  perp_long_short_ratio?: number;
  /**
   * @format double
   */
  perp_liq_volume_24h?: number;
  /**
   * @format double
   */
  perp_oi_contracts?: number;
  /**
   * @format double
   */
  perp_oi_usd?: number;
  /**
   * @format double
   */
  perp_volume_24h_contracts?: number;
  /**
   * @format double
   */
  perp_volume_24h_usd?: number;
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  price_updated_at?: UnixMilliseconds;
  /**
   * @format double
   */
  price_24h?: number;
  /**
   * @format double
   */
  high_24h?: number | null;
  /**
   * @format double
   */
  low_24h?: number | null;
  /**
   * @format double
   */
  amount_24h?: number | null;
}

/**
 * Totals are sums of reported volume/OI.
 */
export interface Totals {
  /**
   * @format double
   */
  total_trades: number;
  /**
   * @format double
   */
  volume_24h: number;
  /**
   * @format double
   */
  volume_7d: number;
  /**
   * @format double
   */
  volume_30d: number;
  /**
   * @format double
   */
  total_oi: number;
}

export interface Ticker {
  /**
   * PERP_<SYMBOL>_USDC.
   */
  symbol: string;
  paxeerSymbol: PriceFeedsAPI.Symbol;
  /**
   * @format double
   */
  mark_price: number;
  /**
   * @format double
   */
  index_price: number;
  /**
   * @format double
   */
  open_interest: number;
  /**
   * @format double
   */
  est_funding_rate: number;
  /**
   * @format double
   */
  last_funding_rate: number;
  /**
   * @format double
   */
  '24h_open': number | null;
  /**
   * @format double
   */
  '24h_close': number;
  /**
   * @format double
   */
  '24h_high': number | null;
  /**
   * @format double
   */
  '24h_low': number | null;
  /**
   * @format double
   */
  '24h_amount': number | null;
  /**
   * Unscaled reported volume.
   * @format double
   */
  '24h_volume': number;
  /**
   * Price ingestion time in Unix milliseconds.
   * @format int64
   */
  updated_at: number | null;
}

/**
 * Estimated USD split of perpetual open interest using the verified account-level exposure.
 */
export interface OpenInterest {
  paxeerSymbol: PriceFeedsAPI.Symbol;
  /**
   * @format double
   */
  long_oi: number;
  /**
   * @format double
   */
  short_oi: number;
  source: 'feed';
  estimated_split: true;
}

export interface WsRuntimeStats {
  snapshotId: string;
  /**
   * @format int64
   */
  totalClients: number;
  /**
   * @format int64
   */
  rejectedConnections: number;
  /**
   * @format int64
   */
  totalBroadcastsSent: number;
  /**
   * @format int64
   */
  totalBroadcastsDropped: number;
  /**
   * @format int64
   */
  incomingMessages: number;
  /**
   * @format int64
   */
  messageRateLimitRejections: number;
  /**
   * @format int64
   */
  binaryMessageRejections: number;
  /**
   * @format int64
   */
  oversizedMessageRejections: number;
  topics: Record<string, number>;
  topicSeq: Record<string, number>;
  limits: WsRuntimeStats.Limits;
}

export namespace WsRuntimeStats {
  export interface Limits {
    /**
     * @format int64
     */
    maxConnections: number;
    /**
     * @format int64
     */
    subscribeRateLimit: number;
    /**
     * @format int64
     */
    maxSubsPerSocket: number;
    /**
     * @format int64
     */
    backpressureBytes: number;
    /**
     * @format int64
     */
    maxPayloadBytes: number;
    /**
     * @format int64
     */
    messageRateLimit: number;
    /**
     * @format int64
     */
    messageBurst: number;
    /**
     * @format int64
     */
    globalMessageRateLimit: number;
    /**
     * @format int64
     */
    globalMessageBurst: number;
  }
}

export interface MarketListResponse {
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  ts: UnixMilliseconds;
  markets: Array<MarketRecord>;
}

export interface MarketRetrieveResponse {
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  ts: UnixMilliseconds;
  /**
   * Display view: volume_24h, open_interest, volume_7d, volume_30d use operator-configured display divisors; reported_volume_24h and reported_open_interest preserve unscaled service values.
   */
  market: MarketRecord;
}

export interface MarketListTotalsResponse {
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  ts: UnixMilliseconds;
  /**
   * Totals are sums of reported volume/OI.
   */
  totals: Totals;
}

export interface MarketListTickersResponse {
  success: true;
  data: Array<Ticker>;
}

export interface MarketRetrieveTickerResponse {
  success: true;
  data: Ticker;
}

export interface MarketListOpenInterestResponse {
  success: true;
  data: Array<OpenInterest>;
}
export declare namespace Markets {
  export {
    type MarketsHealth as MarketsHealth,
    type UnixMilliseconds as UnixMilliseconds,
    type MarketRecord as MarketRecord,
    type Totals as Totals,
    type Ticker as Ticker,
    type OpenInterest as OpenInterest,
    type WsRuntimeStats as WsRuntimeStats,
    type MarketListResponse as MarketListResponse,
    type MarketRetrieveResponse as MarketRetrieveResponse,
    type MarketListTotalsResponse as MarketListTotalsResponse,
    type MarketListTickersResponse as MarketListTickersResponse,
    type MarketRetrieveTickerResponse as MarketRetrieveTickerResponse,
    type MarketListOpenInterestResponse as MarketListOpenInterestResponse,
  };
}
