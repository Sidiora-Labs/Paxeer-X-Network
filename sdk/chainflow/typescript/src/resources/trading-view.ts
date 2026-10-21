// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import { buildHeaders } from '../internal/headers';
import { path as __scalarPath } from '../internal/utils/path';
import type * as CoinGeckoAPI from './coin-gecko';
import type * as PriceFeedsAPI from './price-feeds';

export class TradingView extends APIResource {
  /**
   * Get UDF feed capabilities
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<UdfConfig>} Successful response.
   *
   * @example
   * ```ts
   * const udfConfig = await client.tradingView.listDatafeedConfig('btc');
   * ```
   */
  listDatafeedConfig(market: CoinGeckoAPI.Market, options?: RequestOptions): APIPromise<UdfConfig> {
    return this._client.get(__scalarPath`/api/${market}/config`, options);
  }

  /**
   * An integer encoded as a text response, in seconds. Express currently serves text/html, not JSON.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<TradingViewListFeedTimeResponse>} Unix epoch seconds encoded as decimal text.
   *
   * @example
   * ```ts
   * const response = await client.tradingView.listFeedTime('btc');
   * ```
   */
  listFeedTime(
    market: CoinGeckoAPI.Market,
    options?: RequestOptions,
  ): APIPromise<TradingViewListFeedTimeResponse> {
    return this._client.get(__scalarPath`/api/${market}/time`, {
      ...options,
      headers: buildHeaders([{ Accept: 'text/html' }, options?.headers]),
    });
  }

  /**
   * Unknown or mismatched symbol returns an application error with HTTP 200.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {TradingViewResolveSymbolParams} query - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<TradingViewResolveSymbolResponse>} Successful response.
   *
   * @example
   * ```ts
   * const tradingView = await client.tradingView.resolveSymbol('btc', {
   *   symbol: 'BTC',
   * });
   * ```
   */
  resolveSymbol(
    market: CoinGeckoAPI.Market,
    query: TradingViewResolveSymbolParams,
    options?: RequestOptions,
  ): APIPromise<TradingViewResolveSymbolResponse> {
    return this._client.get(__scalarPath`/api/${market}/symbols`, { query, ...options });
  }

  /**
   * This is not cross-market search. The endpoint ignores UDF exchange/type filters.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {TradingViewSearchParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<TradingViewSearchResponse>} Successful response.
   *
   * @example
   * ```ts
   * const tradingView = await client.tradingView.search('btc', {
   *   query: '',
   *   limit: 30,
   * });
   * ```
   */
  search(
    market: CoinGeckoAPI.Market,
    query: TradingViewSearchParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<TradingViewSearchResponse> {
    return this._client.get(__scalarPath`/api/${market}/search`, { query, ...options });
  }

  /**
   * Ascending, inclusive range. Countback limits rows from the START of that range. Request subsequent windows starting at the last returned timestamp plus one interval; do not assume backward-fill semantics. Open candles can change. UDF unknown-symbol/resolution errors use HTTP 200; malformed parameters use HTTP 400.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {TradingViewListCandleHistoryParams} query - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<TradingViewListCandleHistoryResponse>} Successful response.
   *
   * @example
   * ```ts
   * const tradingView = await client.tradingView.listCandleHistory('btc', {
   *   symbol: 'BTC',
   *   resolution: '1',
   *   from: 1,
   *   to: 1,
   *   countback: 1000,
   * });
   * ```
   */
  listCandleHistory(
    market: CoinGeckoAPI.Market,
    query: TradingViewListCandleHistoryParams,
    options?: RequestOptions,
  ): APIPromise<TradingViewListCandleHistoryResponse> {
    return this._client.get(__scalarPath`/api/${market}/history`, { query, ...options });
  }

  /**
   * Omitting symbols returns an empty d array. Bid and ask both equal candle close; these are not orderbook top-of-book prices.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {TradingViewListQuotesParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<TradingViewListQuotesResponse>} Successful response.
   *
   * @example
   * ```ts
   * const tradingView = await client.tradingView.listQuotes('btc');
   * ```
   */
  listQuotes(
    market: CoinGeckoAPI.Market,
    query: TradingViewListQuotesParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<TradingViewListQuotesResponse> {
    return this._client.get(__scalarPath`/api/${market}/quotes`, { query, ...options });
  }
}

export interface UdfConfig {
  supports_search: boolean;
  supports_group_request: boolean;
  supports_marks: boolean;
  supports_timescale_marks: boolean;
  supports_time: boolean;
  exchanges: Array<UdfConfig.Exchange>;
  symbols_types: Array<UdfConfig.SymbolsType>;
  supported_resolutions: Array<string>;
  currency_codes: Array<string>;
}

export namespace UdfConfig {
  export interface Exchange {
    value: string;
    name: string;
    desc: string;
  }

  export interface SymbolsType {
    name: string;
    value: string;
  }
}

export interface SymbolInfo {
  name: PriceFeedsAPI.Symbol;
  ticker: PriceFeedsAPI.Symbol;
  description: string;
  type: string;
  session: string;
  exchange: string;
  listed_exchange: string;
  timezone: string;
  format: string;
  /**
   * @format int64
   */
  pricescale: number;
  /**
   * @format int64
   */
  minmov: number;
  has_intraday: boolean;
  has_daily: boolean;
  has_weekly_and_monthly: boolean;
  supported_resolutions: Array<string>;
  /**
   * @format int64
   */
  volume_precision: number;
  data_status: string;
  currency_code: string;
}

export interface UdfError {
  s: 'error';
  errmsg: string;
}

export interface SymbolSearchResult {
  symbol: PriceFeedsAPI.Symbol;
  full_name: string;
  description: string;
  exchange: string;
  ticker: PriceFeedsAPI.Symbol;
  type: string;
}

/**
 * Parallel arrays in ascending timestamp order; all arrays have the same length. OHLC is USD.
 */
export interface CandleHistory {
  s: 'ok';
  t: Array<UnixSeconds>;
  o: Array<number>;
  h: Array<number>;
  l: Array<number>;
  c: Array<number>;
  v: Array<number>;
}

/**
 * nextTime, when present, is the last candle strictly before from; it is not a forward page cursor.
 */
export interface HistoryNoData {
  s: 'no_data';
  /**
   * Unix epoch seconds, not milliseconds.
   * @format int64
   * @minimum 0
   */
  nextTime?: UnixSeconds;
}

export interface Quotes {
  s: 'ok';
  d: Array<Quote | MissingQuote>;
}

/**
 * Unix epoch seconds, not milliseconds.
 */
export type UnixSeconds = number;

export interface Quote {
  s: 'ok';
  n: PriceFeedsAPI.Symbol;
  v: QuoteValues;
}

export interface MissingQuote {
  s: 'error';
  n: PriceFeedsAPI.Symbol;
  v: Record<string, unknown>;
}

export interface QuoteValues {
  /**
   * Currently not a computed 24h change.
   * @format double
   */
  ch: number;
  /**
   * Currently not a computed 24h change.
   * @format double
   */
  chp: number;
  short_name: PriceFeedsAPI.Symbol;
  exchange: string;
  description: string;
  /**
   * @format double
   */
  lp: number;
  /**
   * Same as lp; not the orderbook best ask.
   * @format double
   */
  ask: number;
  /**
   * Same as lp; not the orderbook best bid.
   * @format double
   */
  bid: number;
  /**
   * @format double
   */
  open_price: number;
  /**
   * @format double
   */
  high_price: number;
  /**
   * @format double
   */
  low_price: number;
  /**
   * Current candle open, not previous daily close.
   * @format double
   */
  prev_close_price: number;
  /**
   * Current candle volume.
   * @format double
   */
  volume: number;
}

export type TradingViewListFeedTimeResponse = string;

export interface TradingViewResolveSymbolParams {
  /**
   * Uppercase symbol matching the selected market.
   */
  symbol: PriceFeedsAPI.Symbol;
}

export type TradingViewResolveSymbolResponse = TradingViewResolveSymbolResponse.SymbolInfo | UdfError;

export namespace TradingViewResolveSymbolResponse {
  export interface SymbolInfo {
    name: PriceFeedsAPI.Symbol;
    ticker: PriceFeedsAPI.Symbol;
    description: string;
    type: string;
    session: string;
    exchange: string;
    listed_exchange: string;
    timezone: string;
    format: string;
    /**
     * @format int64
     */
    pricescale: number;
    /**
     * @format int64
     */
    minmov: number;
    has_intraday: boolean;
    has_daily: boolean;
    has_weekly_and_monthly: boolean;
    supported_resolutions: Array<string>;
    /**
     * @format int64
     */
    volume_precision: number;
    data_status: string;
    currency_code: string;
  }
}

export interface TradingViewSearchParams {
  /**
   * Case-insensitive substring of symbol or full description.
   * @default ""
   */
  query?: string;
  /**
   * Maximum results; one feed exposes at most one symbol.
   * @default 30
   * @minimum 1
   */
  limit?: number;
}

export type TradingViewSearchResponse = Array<SymbolSearchResult>;

export interface TradingViewListCandleHistoryParams {
  /**
   * Uppercase symbol matching the selected market.
   */
  symbol: PriceFeedsAPI.Symbol;
  /**
   * UDF resolution: numeric strings are minutes; D/1D and W/1W are aliases.
   */
  resolution: '1' | '2' | '3' | '5' | '15' | '30' | '60' | '240' | '480' | 'D' | 'W' | '1D' | '1W';
  /**
   * Inclusive lower bound in Unix seconds.
   * @format int64
   * @minimum 0
   * @maximum 9999999999
   */
  from: number;
  /**
   * Inclusive upper bound in Unix seconds; must be >= from.
   * @format int64
   * @minimum 0
   * @maximum 9999999999
   */
  to: number;
  /**
   * Row limit within from/to. Values above 5000 are clamped to 5000, not rejected. Live-candle merging can add one extra row.
   * @default 1000
   * @format int64
   * @minimum 1
   */
  countback?: number;
}

export type TradingViewListCandleHistoryResponse =
  | TradingViewListCandleHistoryResponse.CandleHistory
  | TradingViewListCandleHistoryResponse.HistoryNoData
  | UdfError;

export namespace TradingViewListCandleHistoryResponse {
  export interface CandleHistory {
    s: 'ok';
    t: Array<UnixSeconds>;
    o: Array<number>;
    h: Array<number>;
    l: Array<number>;
    c: Array<number>;
    v: Array<number>;
  }

  export interface HistoryNoData {
    s: 'no_data';
    /**
     * Unix epoch seconds, not milliseconds.
     * @format int64
     * @minimum 0
     */
    nextTime?: UnixSeconds;
  }
}

export interface TradingViewListQuotesParams {
  /**
   * Comma-separated symbols. Only the selected feed's symbol is returned; other symbols are silently omitted.
   */
  symbols?: Array<PriceFeedsAPI.Symbol>;
}

export type TradingViewListQuotesResponse = TradingViewListQuotesResponse.Quotes | UdfError;

export namespace TradingViewListQuotesResponse {
  export interface Quotes {
    s: 'ok';
    d: Array<Quote | MissingQuote>;
  }
}
export declare namespace TradingView {
  export {
    type UdfConfig as UdfConfig,
    type SymbolInfo as SymbolInfo,
    type UdfError as UdfError,
    type SymbolSearchResult as SymbolSearchResult,
    type CandleHistory as CandleHistory,
    type HistoryNoData as HistoryNoData,
    type Quotes as Quotes,
    type UnixSeconds as UnixSeconds,
    type Quote as Quote,
    type MissingQuote as MissingQuote,
    type QuoteValues as QuoteValues,
    type TradingViewListFeedTimeResponse as TradingViewListFeedTimeResponse,
    type TradingViewResolveSymbolResponse as TradingViewResolveSymbolResponse,
    type TradingViewSearchResponse as TradingViewSearchResponse,
    type TradingViewListCandleHistoryResponse as TradingViewListCandleHistoryResponse,
    type TradingViewListQuotesResponse as TradingViewListQuotesResponse,
    type TradingViewResolveSymbolParams as TradingViewResolveSymbolParams,
    type TradingViewSearchParams as TradingViewSearchParams,
    type TradingViewListCandleHistoryParams as TradingViewListCandleHistoryParams,
    type TradingViewListQuotesParams as TradingViewListQuotesParams,
  };
}
