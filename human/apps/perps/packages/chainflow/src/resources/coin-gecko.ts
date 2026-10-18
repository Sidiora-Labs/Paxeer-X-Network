// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import { path as __scalarPath } from '../internal/utils/path';
import type * as PriceFeedsAPI from './price-feeds';
import type * as TradingViewAPI from './trading-view';
import type * as GatewayAPI from './gateway';

export class CoinGecko extends APIResource {
  /**
   * List local CoinGecko-compatible IDs Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.
   *
   * @param {CoinGeckoListParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinGeckoListResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinGecko = await client.coinGecko.list({
   *   include_platform: false,
   * });
   * ```
   */
  list(
    query: CoinGeckoListParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinGeckoListResponse> {
    return this._client.get('/api/v3/coins/list', { query, ...options });
  }

  /**
   * Get CoinGecko-compatible market data Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.
   *
   * @param {CoinGeckoListMarketsParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinGeckoListMarketsResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinGecko = await client.coinGecko.listMarkets({
   *   vs_currency: 'usd',
   *   page: 1,
   *   per_page: 100,
   *   order: 'market_cap_desc',
   *   sparkline: false,
   *   precision: 'full',
   *   locale: 'en',
   * });
   * ```
   */
  listMarkets(
    query: CoinGeckoListMarketsParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinGeckoListMarketsResponse> {
    return this._client.get('/api/v3/coins/markets', { query, ...options });
  }

  /**
   * Get CoinGecko-compatible coin detail Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.
   *
   * @param {CoinID} id - Local adapter identifier; not an official provider registry ID.
   * @param {CoinGeckoRetrieveParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinDetail>} Successful response.
   *
   * @example
   * ```ts
   * const coinDetail = await client.coinGecko.retrieve('bitcoin', {
   *   localization: false,
   *   tickers: false,
   *   community_data: false,
   *   developer_data: false,
   *   sparkline: false,
   *   market_data: true,
   * });
   * ```
   */
  retrieve(
    id: CoinID,
    query: CoinGeckoRetrieveParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinDetail> {
    return this._client.get(__scalarPath`/api/v3/coins/${id}`, { query, ...options });
  }

  /**
   * Response keys are local IDs unless symbols was supplied, in which case keys are lowercase symbols. Omission of both selectors means all markets. Unsupported or duplicate query parameters are rejected with 400. This is a local compatibility adapter, not the official provider.
   *
   * @param {CoinGeckoListSimplePricesParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<CoinGeckoListSimplePricesResponse>} Successful response.
   *
   * @example
   * ```ts
   * const coinGecko = await client.coinGecko.listSimplePrices({
   *   vs_currencies: 'usd',
   *   include_market_cap: false,
   *   include_24hr_vol: false,
   *   include_24hr_change: false,
   *   include_last_updated_at: false,
   * });
   * ```
   */
  listSimplePrices(
    query: CoinGeckoListSimplePricesParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<CoinGeckoListSimplePricesResponse> {
    return this._client.get('/api/v3/simple/price', { query, ...options });
  }
}

export interface CoinListItem {
  id: CoinID;
  symbol: Market;
  name: string;
}

/**
 * CoinGecko-compatible local-feed metadata.
 */
export interface CoinMarket {
  id: CoinID;
  symbol: Market;
  name: string;
  /**
   * @format uri
   */
  image: string | null;
  /**
   * @format double
   */
  current_price: number;
  /**
   * @format double
   */
  market_cap: number | null;
  /**
   * @format double
   */
  market_cap_rank: number | null;
  /**
   * @format double
   */
  fully_diluted_valuation: number | null;
  /**
   * @format double
   */
  total_volume: number | null;
  /**
   * @format double
   */
  high_24h: number | null;
  /**
   * @format double
   */
  low_24h: number | null;
  /**
   * @format double
   */
  price_change_24h: number | null;
  /**
   * @format double
   */
  price_change_percentage_24h: number | null;
  /**
   * @format double
   */
  market_cap_change_24h: number | null;
  /**
   * @format double
   */
  market_cap_change_percentage_24h: number | null;
  /**
   * @format double
   */
  circulating_supply: number | null;
  /**
   * @format double
   */
  total_supply: number | null;
  /**
   * @format double
   */
  max_supply: number | null;
  /**
   * @format double
   */
  ath: number | null;
  /**
   * @format double
   */
  ath_change_percentage: number | null;
  /**
   * @format double
   */
  atl: number | null;
  /**
   * @format double
   */
  atl_change_percentage: number | null;
  /**
   * @format date-time
   */
  ath_date: string | null;
  /**
   * @format date-time
   */
  atl_date: string | null;
  roi: Record<string, unknown> | null;
  /**
   * @format date-time
   */
  last_updated: GatewayAPI.IsoTimestamp;
  /**
   * @format double
   */
  price_change_percentage_1h_in_currency?: number | null;
  /**
   * @format double
   */
  price_change_percentage_24h_in_currency?: number | null;
}

export interface CoinDetail {
  id: CoinID;
  symbol: Market;
  name: string;
  image: CoinDetail.Image;
  /**
   * @format int64
   */
  market_cap_rank: number | null;
  market_data: CoinDetail.MarketData;
  /**
   * @format date-time
   */
  last_updated: GatewayAPI.IsoTimestamp;
}

export namespace CoinDetail {
  export interface Image {
    thumb: string | null;
    small: string | null;
    large: string | null;
  }

  export interface MarketData {
    current_price: MarketData.CurrentPrice;
    market_cap: MarketData.MarketCap;
    fully_diluted_valuation: MarketData.FullyDilutedValuation;
    total_volume: MarketData.TotalVolume;
    high_24h: MarketData.High24h;
    low_24h: MarketData.Low24h;
    ath: MarketData.Ath;
    ath_change_percentage: MarketData.AthChangePercentage;
    ath_date: MarketData.AthDate;
    atl: MarketData.Atl;
    atl_change_percentage: MarketData.AtlChangePercentage;
    atl_date: MarketData.AtlDate;
    /**
     * @format double
     */
    market_cap_rank: number | null;
    /**
     * @format double
     */
    price_change_24h: number | null;
    /**
     * @format double
     */
    price_change_percentage_24h: number | null;
    /**
     * @format double
     */
    market_cap_change_24h: number | null;
    /**
     * @format double
     */
    market_cap_change_percentage_24h: number | null;
    /**
     * @format double
     */
    circulating_supply: number | null;
    /**
     * @format double
     */
    total_supply: number | null;
    /**
     * @format double
     */
    max_supply: number | null;
    /**
     * @format date-time
     */
    last_updated: GatewayAPI.IsoTimestamp;
  }

  export namespace MarketData {
    export interface CurrentPrice {
      /**
       * @format double
       */
      usd: number;
    }

    export interface MarketCap {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface FullyDilutedValuation {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface TotalVolume {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface High24h {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface Low24h {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface Ath {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface AthChangePercentage {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface AthDate {
      /**
       * @format date-time
       */
      usd: string | null;
    }

    export interface Atl {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface AtlChangePercentage {
      /**
       * @format double
       */
      usd: number | null;
    }

    export interface AtlDate {
      /**
       * @format date-time
       */
      usd: string | null;
    }
  }
}

export interface CoinSimpleQuote {
  /**
   * @format double
   */
  usd: number;
  /**
   * @format double
   */
  usd_market_cap?: number | null;
  /**
   * @format double
   */
  usd_24h_vol?: number | null;
  /**
   * @format double
   */
  usd_24h_change?: number | null;
  /**
   * Unix epoch seconds, not milliseconds.
   * @format int64
   * @minimum 0
   */
  last_updated_at?: TradingViewAPI.UnixSeconds;
}

export type CoinID =
  | 'bitcoin'
  | 'ethereum'
  | 'solana'
  | 'avalanche-2'
  | 'chainlink'
  | 'hyperliquid'
  | 'ripple'
  | 'aster'
  | 'official-trump'
  | 'binancecoin'
  | 'sidiora'
  | 'paxeer'
  | 'tesla'
  | 'nvidia'
  | 'alphabet'
  | 'gold'
  | 'nasdaq-100'
  | 'sp-500';

export type Market =
  | 'btc'
  | 'eth'
  | 'sol'
  | 'avax'
  | 'link'
  | 'hype'
  | 'xrp'
  | 'aster'
  | 'trump'
  | 'bnb'
  | 'sid'
  | 'pax'
  | 'tsla'
  | 'nvda'
  | 'googl'
  | 'xau'
  | 'nas100'
  | 'spx500';

export interface CoinGeckoListParams {
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  include_platform?: boolean;
}

export type CoinGeckoListResponse = Array<CoinListItem>;

export interface CoinGeckoListMarketsParams {
  /**
   * Only USD is supported.
   * @default usd
   */
  vs_currency?: 'usd' | 'USD';
  /**
   * Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols.
   * @maxItems 18
   */
  ids?: Array<CoinID>;
  /**
   * Comma-separated symbols; case-insensitive. Mutually exclusive with ids.
   * @maxItems 18
   */
  symbols?: Array<Market>;
  /**
   * One-based page.
   * @default 1
   * @minimum 1
   * @maximum 1000000
   */
  page?: number;
  /**
   * Page size.
   * @default 100
   * @minimum 1
   * @maximum 250
   */
  per_page?: number;
  /**
   * Unavailable values sort last; ID breaks ties.
   * @default market_cap_desc
   */
  order?: 'market_cap_asc' | 'market_cap_desc' | 'volume_asc' | 'volume_desc' | 'id_asc' | 'id_desc';
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  sparkline?: boolean;
  /**
   * Optional extra percentage-change fields.
   */
  price_change_percentage?: Array<'1h' | '24h'>;
  /**
   * full or decimal count 0..18.
   * @default full
   * @pattern ^(full|[0-9]|1[0-8])$
   */
  precision?: string;
  /**
   * Only English metadata is supported.
   * @default en
   */
  locale?: 'en';
}

export type CoinGeckoListMarketsResponse = Array<CoinMarket>;

export interface CoinGeckoRetrieveParams {
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  localization?: boolean;
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  tickers?: boolean;
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  community_data?: boolean;
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  developer_data?: boolean;
  /**
   * Only false is supported; true returns 400.
   * @default false
   */
  sparkline?: boolean;
  /**
   * Must be true.
   * @default true
   */
  market_data?: true;
}

export interface CoinGeckoListSimplePricesParams {
  /**
   * Comma-separated local IDs. Omit for all markets. Mutually exclusive with symbols.
   * @maxItems 18
   */
  ids?: Array<CoinID>;
  /**
   * Comma-separated symbols; case-insensitive. Mutually exclusive with ids.
   * @maxItems 18
   */
  symbols?: Array<Market>;
  /**
   * Only one currency, USD.
   * @default usd
   */
  vs_currencies?: 'usd' | 'USD';
  /**
   * Include the corresponding optional response field.
   * @default false
   */
  include_market_cap?: boolean;
  /**
   * Include the corresponding optional response field.
   * @default false
   */
  include_24hr_vol?: boolean;
  /**
   * Include the corresponding optional response field.
   * @default false
   */
  include_24hr_change?: boolean;
  /**
   * Include the corresponding optional response field.
   * @default false
   */
  include_last_updated_at?: boolean;
}

export type CoinGeckoListSimplePricesResponse = Record<string, CoinSimpleQuote>;
export declare namespace CoinGecko {
  export {
    type CoinListItem as CoinListItem,
    type CoinMarket as CoinMarket,
    type CoinDetail as CoinDetail,
    type CoinSimpleQuote as CoinSimpleQuote,
    type CoinID as CoinID,
    type Market as Market,
    type CoinGeckoListResponse as CoinGeckoListResponse,
    type CoinGeckoListMarketsResponse as CoinGeckoListMarketsResponse,
    type CoinGeckoListSimplePricesResponse as CoinGeckoListSimplePricesResponse,
    type CoinGeckoListParams as CoinGeckoListParams,
    type CoinGeckoListMarketsParams as CoinGeckoListMarketsParams,
    type CoinGeckoRetrieveParams as CoinGeckoRetrieveParams,
    type CoinGeckoListSimplePricesParams as CoinGeckoListSimplePricesParams,
  };
}
