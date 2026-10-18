// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import { path as __scalarPath } from '../internal/utils/path';
import type * as CoinGeckoAPI from './coin-gecko';
import type * as TradingViewAPI from './trading-view';
import type * as MarketsAPI from './markets';

export class PriceFeeds extends APIResource {
  /**
   * HTTP 200 may contain {error: No price data available} during cold start. timestamp is candle-bucket start.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<PriceFeedListResponse>} Successful response.
   *
   * @example
   * ```ts
   * const priceFeed = await client.priceFeeds.list('btc');
   * ```
   */
  list(market: CoinGeckoAPI.Market, options?: RequestOptions): APIPromise<PriceFeedListResponse> {
    return this._client.get(__scalarPath`/api/${market}/price`, options);
  }

  /**
   * Get feed
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<FeedHealth>} Successful response.
   *
   * @example
   * ```ts
   * const feedHealth = await client.priceFeeds.listHealth('btc');
   * ```
   */
  listHealth(market: CoinGeckoAPI.Market, options?: RequestOptions): APIPromise<FeedHealth> {
    return this._client.get(__scalarPath`/api/${market}/health`, options);
  }

  /**
   * Get spot-style market statistics
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<Stats>} Successful response.
   *
   * @example
   * ```ts
   * const stats = await client.priceFeeds.listSpotStyleStats('btc');
   * ```
   */
  listSpotStyleStats(market: CoinGeckoAPI.Market, options?: RequestOptions): APIPromise<Stats> {
    return this._client.get(__scalarPath`/api/${market}/stats`, options);
  }

  /**
   * Get perpetual-market statistics
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<PerpStats>} Successful response.
   *
   * @example
   * ```ts
   * const perpStats = await client.priceFeeds.listPerpetualStats('btc');
   * ```
   */
  listPerpetualStats(market: CoinGeckoAPI.Market, options?: RequestOptions): APIPromise<PerpStats> {
    return this._client.get(__scalarPath`/api/${market}/perp_stats`, options);
  }

  /**
   * This path reports tape storage status; use /trades/{symbol} for trade records.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<TradeTapeStatus>} Successful response.
   *
   * @example
   * ```ts
   * const tradeTapeStatus = await client.priceFeeds.listTradeTapeStatus('btc');
   * ```
   */
  listTradeTapeStatus(market: CoinGeckoAPI.Market, options?: RequestOptions): APIPromise<TradeTapeStatus> {
    return this._client.get(__scalarPath`/api/${market}/trades`, options);
  }

  /**
   * Get recent spot trades
   *
   * @param {string} symbol_ - Trade tape symbol; normalized to uppercase. Unknown symbols generally return an empty tape.
   * @param {PriceFeedRetrieveRecentSpotTradesParams} params - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<SpotTrades>} Successful response.
   *
   * @example
   * ```ts
   * const spotTrades = await client.priceFeeds.retrieveRecentSpotTrades('symbol', {
   *   market: 'btc',
   *   limit: 100,
   * });
   * ```
   */
  retrieveRecentSpotTrades(
    symbol_: string,
    params: PriceFeedRetrieveRecentSpotTradesParams,
    options?: RequestOptions,
  ): APIPromise<SpotTrades> {
    const { market, ...query } = params;
    return this._client.get(__scalarPath`/api/${market}/trades/${symbol_}`, { query, ...options });
  }

  /**
   * Get recent perpetual trades
   *
   * @param {string} symbol_ - Trade tape symbol; normalized to uppercase. Unknown symbols generally return an empty tape.
   * @param {PriceFeedRetrieveRecentPerpetualTradesParams} params - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<PerpTrades>} Successful response.
   *
   * @example
   * ```ts
   * const perpTrades = await client.priceFeeds.retrieveRecentPerpetualTrades('symbol', {
   *   market: 'btc',
   *   limit: 100,
   * });
   * ```
   */
  retrieveRecentPerpetualTrades(
    symbol_: string,
    params: PriceFeedRetrieveRecentPerpetualTradesParams,
    options?: RequestOptions,
  ): APIPromise<PerpTrades> {
    const { market, ...query } = params;
    return this._client.get(__scalarPath`/api/${market}/perp_trades/${symbol_}`, { query, ...options });
  }
}

export interface Price {
  symbol: Symbol;
  /**
   * Current feed close in USD.
   * @format double
   */
  price: number;
  /**
   * Start of the current/latest 1-minute candle, NOT the exact quote tick time.
   * @format date-time
   */
  timestamp: string;
  /**
   * @format double
   */
  open: number;
  /**
   * @format double
   */
  high: number;
  /**
   * @format double
   */
  low: number;
}

export interface Error {
  error: string;
}

export interface FeedHealth {
  status: 'ok';
  /**
   * Stored backfill status; null if not set.
   */
  backfill: string | null;
  /**
   * @format date-time
   */
  lastCandle: string | null;
  symbol: Symbol;
}

export interface Stats {
  symbol: Symbol;
  /**
   * Rolling USD volume from trade tape.
   * @format double
   */
  volume_24h: number;
  /**
   * USD open interest.
   * @format double
   */
  open_interest: number;
  /**
   * Fractional rate, e.g. 0.0001 means 0.01%.
   * @format double
   */
  est_funding_rate: number;
  /**
   * Last 8-hour settlement rate, as a fraction.
   * @format double
   */
  last_funding_rate: number;
}

/**
 * perpetual-market statistics. basis_bps is basis points; funding rates are fractions. liq_volume_24h, oi_usd and volume_24h_usd are USD notionals.
 */
export interface PerpStats {
  symbol: Symbol;
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
  basis_bps: number;
  /**
   * @format double
   */
  funding_rate: number;
  /**
   * @format double
   */
  last_funding_rate: number;
  /**
   * @format double
   */
  long_short_ratio: number;
  /**
   * @format double
   */
  liq_volume_24h: number;
  /**
   * @format double
   */
  oi_contracts: number;
  /**
   * @format double
   */
  oi_usd: number;
  /**
   * @format double
   */
  volume_24h_contracts: number;
  /**
   * @format double
   */
  volume_24h_usd: number;
  /**
   * Remaining milliseconds until funding; a duration, NOT an epoch timestamp.
   * @format int64
   */
  next_funding_ms: number;
}

export interface TradeTapeStatus {
  configured: boolean;
  connected: boolean;
  ringSymbols: Array<string>;
  ringSizes: Record<string, number>;
}

export interface SpotTrades {
  /**
   * Requested symbol, normalized to uppercase.
   */
  symbol: string;
  /**
   * @format int64
   */
  count: number;
  trades: Array<SpotTrade>;
}

export interface PerpTrades {
  symbol: string;
  /**
   * @format int64
   */
  count: number;
  trades: Array<PerpTrade>;
}

export type Symbol =
  | 'BTC'
  | 'ETH'
  | 'SOL'
  | 'AVAX'
  | 'LINK'
  | 'HYPE'
  | 'XRP'
  | 'ASTER'
  | 'TRUMP'
  | 'BNB'
  | 'SID'
  | 'PAX'
  | 'TSLA'
  | 'NVDA'
  | 'GOOGL'
  | 'XAU'
  | 'NAS100'
  | 'SPX500';

/**
 * orderbook-crossing trade.
 */
export interface SpotTrade {
  symbol: Symbol;
  side: 'BUY' | 'SELL';
  /**
   * @format double
   */
  price: number;
  /**
   * @format double
   */
  size: number;
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  trade_time: MarketsAPI.UnixMilliseconds;
  /**
   * .
   */
  id?: string;
}

export interface PerpTrade {
  symbol: Symbol;
  side: 'BUY' | 'SELL';
  /**
   * @format double
   */
  price: number;
  /**
   * @format double
   */
  contracts: number;
  /**
   * USD notional.
   * @format double
   */
  notional: number;
  liquidation: boolean;
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  trade_time: MarketsAPI.UnixMilliseconds;
  /**
   * Optional Dragonfly stream ID.
   */
  id?: string;
  /**
   * Optional alias for contracts.
   * @format double
   */
  size?: number;
}

export type PriceFeedListResponse = PriceFeedListResponse.Price | Error;

export namespace PriceFeedListResponse {
  export interface Price {
    symbol: Symbol;
    /**
     * Current feed close in USD.
     * @format double
     */
    price: number;
    /**
     * Start of the current/latest 1-minute candle, NOT the exact quote tick time.
     * @format date-time
     */
    timestamp: string;
    /**
     * @format double
     */
    open: number;
    /**
     * @format double
     */
    high: number;
    /**
     * @format double
     */
    low: number;
  }
}

export interface PriceFeedRetrieveRecentSpotTradesParams {
  /**
   * Path param: Lowercase feed identifier. This route addresses ONE feed, not all markets.
   */
  market: CoinGeckoAPI.Market;
  /**
   * Query param: Most recent trades, newest first.
   * @default 100
   * @minimum 1
   * @maximum 1000
   */
  limit?: number;
}

export interface PriceFeedRetrieveRecentPerpetualTradesParams {
  /**
   * Path param: Lowercase feed identifier. This route addresses ONE feed, not all markets.
   */
  market: CoinGeckoAPI.Market;
  /**
   * Query param: Most recent trades, newest first.
   * @default 100
   * @minimum 1
   * @maximum 1000
   */
  limit?: number;
}
export declare namespace PriceFeeds {
  export {
    type Price as Price,
    type Error as Error,
    type FeedHealth as FeedHealth,
    type Stats as Stats,
    type PerpStats as PerpStats,
    type TradeTapeStatus as TradeTapeStatus,
    type SpotTrades as SpotTrades,
    type PerpTrades as PerpTrades,
    type Symbol as Symbol,
    type SpotTrade as SpotTrade,
    type PerpTrade as PerpTrade,
    type PriceFeedListResponse as PriceFeedListResponse,
    type PriceFeedRetrieveRecentSpotTradesParams as PriceFeedRetrieveRecentSpotTradesParams,
    type PriceFeedRetrieveRecentPerpetualTradesParams as PriceFeedRetrieveRecentPerpetualTradesParams,
  };
}
