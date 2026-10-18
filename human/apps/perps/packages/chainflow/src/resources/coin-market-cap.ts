// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import type * as GatewayAPI from './gateway';
import type * as PriceFeedsAPI from './price-feeds';
import type * as CoinGeckoAPI from './coin-gecko';
import type * as TradingViewAPI from './trading-view';

export class CoinMarketCap extends APIResource {
  /**
   * Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.
   *
   * @param {CoinMarketCapListQuotesParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinMarketCapListQuotesResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinMarketCap = await client.coinMarketCap.listQuotes({
   *   convert: 'USD',
   * });
   * ```
   */
  listQuotes(
    query: CoinMarketCapListQuotesParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinMarketCapListQuotesResponse> {
    return this._client.get('/v1/cryptocurrency/quotes/latest', { query, ...options });
  }

  /**
   * Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.
   *
   * @param {CoinMarketCapListListingsParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinMarketCapListListingsResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinMarketCap = await client.coinMarketCap.listListings({
   *   convert: 'USD',
   *   start: 1,
   *   limit: 100,
   *   sort: 'market_cap',
   *   sort_dir: 'desc',
   * });
   * ```
   */
  listListings(
    query: CoinMarketCapListListingsParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinMarketCapListListingsResponse> {
    return this._client.get('/v1/cryptocurrency/listings/latest', { query, ...options });
  }

  /**
   * Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.
   *
   * @param {CoinMarketCapListQuotesAPIAliasParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinMarketCapListQuotesAPIAliasResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinMarketCap = await client.coinMarketCap.listQuotesAPIAlias({
   *   convert: 'USD',
   * });
   * ```
   */
  listQuotesAPIAlias(
    query: CoinMarketCapListQuotesAPIAliasParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinMarketCapListQuotesAPIAliasResponse> {
    return this._client.get('/api/v1/cryptocurrency/quotes/latest', { query, ...options });
  }

  /**
   * Alias of the same path without /api. Only USD; unknown/duplicate parameters are rejected. Quotes are keyed by uppercase symbol, not numeric provider ID. Missing selected prices fail the whole request. Unsupported metadata is null.
   *
   * @param {CoinMarketCapListListingsAPIAliasParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinMarketCapListListingsAPIAliasResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinMarketCap = await client.coinMarketCap.listListingsAPIAlias({
   *   convert: 'USD',
   *   start: 1,
   *   limit: 100,
   *   sort: 'market_cap',
   *   sort_dir: 'desc',
   * });
   * ```
   */
  listListingsAPIAlias(
    query: CoinMarketCapListListingsAPIAliasParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinMarketCapListListingsAPIAliasResponse> {
    return this._client.get('/api/v1/cryptocurrency/listings/latest', { query, ...options });
  }
}

export interface CmcStatus {
  /**
   * @format date-time
   */
  timestamp: GatewayAPI.IsoTimestamp;
  /**
   * @format int64
   */
  error_code: number;
  error_message: string | null;
  /**
   * Elapsed handler time in milliseconds.
   * @format int64
   */
  elapsed: number;
  credit_count: 0;
  notice: string | null;
}

export interface CmcAsset {
  /**
   * No official CMC ID is assigned; always null.
   * @format int64
   */
  id: number | null;
  name: string;
  symbol: PriceFeedsAPI.Symbol;
  slug: CoinGeckoAPI.CoinID;
  /**
   * @format int64
   */
  num_market_pairs: number | null;
  /**
   * @format date-time
   */
  date_added: string | null;
  tags: Array<string>;
  /**
   * @format double
   */
  max_supply: number | null;
  /**
   * @format double
   */
  circulating_supply: number | null;
  /**
   * @format double
   */
  total_supply: number | null;
  infinite_supply: boolean | null;
  platform: Record<string, unknown> | null;
  /**
   * @format int64
   */
  cmc_rank: number | null;
  /**
   * @format double
   */
  self_reported_circulating_supply: number | null;
  /**
   * @format double
   */
  self_reported_market_cap: number | null;
  /**
   * @format date-time
   */
  last_updated: GatewayAPI.IsoTimestamp;
  quote: CmcAsset.Quote;
}

export namespace CmcAsset {
  export interface Quote {
    USD: CmcUsdQuote;
  }
}

export interface CmcUsdQuote {
  /**
   * @format double
   */
  price: number;
  /**
   * @format double
   */
  volume_24h: number | null;
  /**
   * @format double
   */
  volume_change_24h: number | null;
  /**
   * @format double
   */
  percent_change_1h: number | null;
  /**
   * @format double
   */
  percent_change_24h: number | null;
  /**
   * @format double
   */
  percent_change_7d: number | null;
  /**
   * @format double
   */
  percent_change_30d: number | null;
  /**
   * @format double
   */
  percent_change_60d: number | null;
  /**
   * @format double
   */
  percent_change_90d: number | null;
  /**
   * @format double
   */
  market_cap: number | null;
  /**
   * @format double
   */
  market_cap_dominance: number | null;
  /**
   * @format double
   */
  fully_diluted_market_cap: number | null;
  /**
   * @format double
   */
  tvl: number | null;
  /**
   * @format date-time
   */
  last_updated: GatewayAPI.IsoTimestamp;
}

export interface CoinMarketCapListQuotesParams {
  /**
   * Only USD is supported.
   * @default USD
   */
  convert?: 'USD' | 'usd';
  /**
   * Comma-separated symbols; case-insensitive. Mutually exclusive with slug.
   */
  symbol?: Array<PriceFeedsAPI.Symbol>;
  /**
   * Comma-separated local IDs. Mutually exclusive with symbol.
   */
  slug?: Array<CoinGeckoAPI.CoinID>;
}

export interface CoinMarketCapListQuotesResponse {
  status: CmcStatus;
  data: Record<string, CmcAsset>;
}

export interface CoinMarketCapListListingsParams {
  /**
   * Only USD is supported.
   * @default USD
   */
  convert?: 'USD' | 'usd';
  /**
   * One-based offset.
   * @default 1
   * @minimum 1
   * @maximum 1000000
   */
  start?: number;
  /**
   * Page size; there are 18 local markets.
   * @default 100
   * @minimum 1
   * @maximum 5000
   */
  limit?: number;
  /**
   * Unavailable values sort last.
   * @default market_cap
   */
  sort?: 'market_cap' | 'price' | 'name' | 'symbol' | 'volume_24h' | 'percent_change_24h';
  /**
   * Sort direction.
   * @default desc
   */
  sort_dir?: 'asc' | 'desc';
}

export interface CoinMarketCapListListingsResponse {
  status: CmcStatus;
  data: Array<CmcAsset>;
}

export interface CoinMarketCapListQuotesAPIAliasParams {
  /**
   * Only USD is supported.
   * @default USD
   */
  convert?: 'USD' | 'usd';
  /**
   * Comma-separated symbols; case-insensitive. Mutually exclusive with slug.
   */
  symbol?: Array<PriceFeedsAPI.Symbol>;
  /**
   * Comma-separated local IDs. Mutually exclusive with symbol.
   */
  slug?: Array<CoinGeckoAPI.CoinID>;
}

export interface CoinMarketCapListQuotesAPIAliasResponse {
  status: CmcStatus;
  data: Record<string, CmcAsset>;
}

export interface CoinMarketCapListListingsAPIAliasParams {
  /**
   * Only USD is supported.
   * @default USD
   */
  convert?: 'USD' | 'usd';
  /**
   * One-based offset.
   * @default 1
   * @minimum 1
   * @maximum 1000000
   */
  start?: number;
  /**
   * Page size; there are 18 local markets.
   * @default 100
   * @minimum 1
   * @maximum 5000
   */
  limit?: number;
  /**
   * Unavailable values sort last.
   * @default market_cap
   */
  sort?: 'market_cap' | 'price' | 'name' | 'symbol' | 'volume_24h' | 'percent_change_24h';
  /**
   * Sort direction.
   * @default desc
   */
  sort_dir?: 'asc' | 'desc';
}

export interface CoinMarketCapListListingsAPIAliasResponse {
  status: CmcStatus;
  data: Array<CmcAsset>;
}
export declare namespace CoinMarketCap {
  export {
    type CmcStatus as CmcStatus,
    type CmcAsset as CmcAsset,
    type CmcUsdQuote as CmcUsdQuote,
    type CoinMarketCapListQuotesResponse as CoinMarketCapListQuotesResponse,
    type CoinMarketCapListListingsResponse as CoinMarketCapListListingsResponse,
    type CoinMarketCapListQuotesAPIAliasResponse as CoinMarketCapListQuotesAPIAliasResponse,
    type CoinMarketCapListListingsAPIAliasResponse as CoinMarketCapListListingsAPIAliasResponse,
    type CoinMarketCapListQuotesParams as CoinMarketCapListQuotesParams,
    type CoinMarketCapListListingsParams as CoinMarketCapListListingsParams,
    type CoinMarketCapListQuotesAPIAliasParams as CoinMarketCapListQuotesAPIAliasParams,
    type CoinMarketCapListListingsAPIAliasParams as CoinMarketCapListListingsAPIAliasParams,
  };
}
