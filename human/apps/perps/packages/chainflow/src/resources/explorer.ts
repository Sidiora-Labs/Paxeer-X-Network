// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import type * as MarketsAPI from './markets';
import type * as PriceFeedsAPI from './price-feeds';
import type * as TradingViewAPI from './trading-view';
import type * as GatewayAPI from './gateway';

export class Explorer extends APIResource {
  /**
   * Get EVM explorer health
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<ExplorerHealth>} Successful response.
   *
   * @example
   * ```ts
   * const explorerHealth = await client.explorer.listHealth();
   * ```
   */
  listHealth(options?: RequestOptions): APIPromise<ExplorerHealth> {
    return this._client.get('/api/explorer/health', options);
  }

  /**
   * In-memory snapshot; may be empty on startup or immediately after reorg/reset. Not an archival chain API.
   *
   * @param {ExplorerListBlocksParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<ExplorerListBlocksResponse>} Successful response.
   *
   * @example
   * ```ts
   * const explorer = await client.explorer.listBlocks({
   *   limit: 20,
   * });
   * ```
   */
  listBlocks(
    query: ExplorerListBlocksParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<ExplorerListBlocksResponse> {
    return this._client.get('/api/explorer/blocks', { query, ...options });
  }

  /**
   * Explorer activity.
   *
   * @param {ExplorerListTransactionsParams} [query] - The parameters to send with the request.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<ExplorerListTransactionsResponse>} Successful response.
   *
   * @example
   * ```ts
   * const explorer = await client.explorer.listTransactions({
   *   limit: 50,
   * });
   * ```
   */
  listTransactions(
    query: ExplorerListTransactionsParams | null | undefined = {},
    options?: RequestOptions,
  ): APIPromise<ExplorerListTransactionsResponse> {
    return this._client.get('/api/explorer/transactions', { query, ...options });
  }
}

export interface ExplorerHealth {
  status: 'ok' | 'degraded';
  chain_id: 125;
  /**
   * @format int64
   */
  latest_block: number | null;
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  ts: MarketsAPI.UnixMilliseconds;
  chain_connected: boolean;
  /**
   * @format int64
   */
  blocks_seen: number;
  /**
   * @format int64
   */
  txs_seen: number;
}

/**
 * Block from EVM chain 125. Newest-first in snapshots.
 */
export interface Block {
  /**
   * @format int64
   */
  block_number: number;
  /**
   * Unix epoch milliseconds.
   * @format int64
   * @minimum 0
   */
  time: MarketsAPI.UnixMilliseconds;
  /**
   * EVM miner/proposer address.
   */
  proposer: string;
  /**
   * @pattern ^0x[0-9a-fA-F]{64}$
   */
  hash: string;
  /**
   * On-chain transaction count.
   * @format int64
   */
  transactions: number;
}

/**
 * Block is the latest observed block or null. Prices are authoritative pricefeed quotes.
 */
export interface Transaction {
  /**
   * @pattern ^0x[0-9a-fA-F]{64}$
   */
  hash: string;
  action:
    | 'OROB Fill'
    | 'Place Order'
    | 'Cancel Order'
    | 'Modify Order'
    | 'Oracle Updated'
    | 'Sealed Batch Executed';
  /**
   * @format int64
   */
  block: number | null;
  /**
   * @format date-time
   */
  time: GatewayAPI.IsoTimestamp;
  /**
   * pax1.
   */
  user: string;
  symbol: PriceFeedsAPI.Symbol;
  side: 'buy' | 'sell';
  /**
   * @format double
   */
  price: number;
  /**
   * @format double
   */
  size: number;
}

export interface ExplorerListBlocksParams {
  /**
   * Newest-first count. Numeric values are clamped to 1..50.
   * @default 20
   * @minimum 1
   * @maximum 50
   */
  limit?: number;
}

export interface ExplorerListBlocksResponse {
  blocks: Array<Block>;
}

export interface ExplorerListTransactionsParams {
  /**
   * Newest-first count. Numeric values are clamped to 1..200.
   * @default 50
   * @minimum 1
   * @maximum 200
   */
  limit?: number;
}

export interface ExplorerListTransactionsResponse {
  transactions: Array<Transaction>;
}
export declare namespace Explorer {
  export {
    type ExplorerHealth as ExplorerHealth,
    type Block as Block,
    type Transaction as Transaction,
    type ExplorerListBlocksResponse as ExplorerListBlocksResponse,
    type ExplorerListTransactionsResponse as ExplorerListTransactionsResponse,
    type ExplorerListBlocksParams as ExplorerListBlocksParams,
    type ExplorerListTransactionsParams as ExplorerListTransactionsParams,
  };
}
