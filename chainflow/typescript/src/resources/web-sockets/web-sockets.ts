// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../../resource';
import { APIPromise } from '../../api-promise';
import type { RequestOptions } from '../../internal/request-options';
import { path as __scalarPath } from '../../internal/utils/path';
import type * as CoinGeckoAPI from '../coin-gecko';
import type * as PriceFeedsAPI from '../price-feeds';
import {
  WebSocketsConnectPriceFeedWS,
  type WebSocketsConnectPriceFeedWSClientOptions,
} from './connectPriceFeed-ws';
import {
  WebSocketsConnectMarketsWS,
  type WebSocketsConnectMarketsWSClientOptions,
} from './connectMarkets-ws';
import {
  WebSocketsConnectExplorerWS,
  type WebSocketsConnectExplorerWSClientOptions,
} from './connectExplorer-ws';
import {
  WebSocketsConnectExplorerAliasWS,
  type WebSocketsConnectExplorerAliasWSClientOptions,
} from './connectExplorerAlias-ws';

export class WebSockets extends APIResource {
  /**
   * One connection addresses one feed. SYMBOL is the uppercase counterpart of lowercase market; no cross-symbol subscriptions. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.
   *
   * @param {CoinGeckoAPI.Market} market - Lowercase feed identifier. This route addresses ONE feed, not all markets.
   * @param {WebSocketsConnectPriceFeedWSClientOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {WebSocketsConnectPriceFeedWS} Switching Protocols; subsequent frames follow x-websocket.
   *
   * @example
   * ```ts
   * const connection = client.webSockets.connectPriceFeed('btc');
   *
   * try {
   *   for await (const message of connection) {
   *     console.log(message);
   *   }
   * } finally {
   *   connection.close();
   * }
   * ```
   */
  connectPriceFeed(
    market: CoinGeckoAPI.Market,
    options?: WebSocketsConnectPriceFeedWSClientOptions,
  ): WebSocketsConnectPriceFeedWS {
    return new WebSocketsConnectPriceFeedWS(this._client, { market: market }, options);
  }

  /**
   * Aggregate socket. Initial snapshots and full replacements; default broadcast cadence is 5 seconds. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.
   *
   * @param {WebSocketsConnectMarketsWSClientOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {WebSocketsConnectMarketsWS} Switching Protocols; subsequent frames follow x-websocket.
   *
   * @example
   * ```ts
   * const connection = client.webSockets.connectMarkets();
   *
   * try {
   *   for await (const message of connection) {
   *     console.log(message);
   *   }
   * } finally {
   *   connection.close();
   * }
   * ```
   */
  connectMarkets(options?: WebSocketsConnectMarketsWSClientOptions): WebSocketsConnectMarketsWS {
    return new WebSocketsConnectMarketsWS(this._client, {}, options);
  }

  /**
   * EVM blocks and a separate activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.
   *
   * @param {WebSocketsConnectExplorerWSClientOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {WebSocketsConnectExplorerWS} Switching Protocols; subsequent frames follow x-websocket.
   *
   * @example
   * ```ts
   * const connection = client.webSockets.connectExplorer();
   *
   * try {
   *   for await (const message of connection) {
   *     console.log(message);
   *   }
   * } finally {
   *   connection.close();
   * }
   * ```
   */
  connectExplorer(options?: WebSocketsConnectExplorerWSClientOptions): WebSocketsConnectExplorerWS {
    return new WebSocketsConnectExplorerWS(this._client, {}, options);
  }

  /**
   * Alias of /ws/explorer. EVM blocks and a separate  activity stream. This upgrades the connection; there is no HTTP JSON response body on success. Standard REST SDK generators cannot implement the stream automatically. Use x-websocket for the protocol.
   *
   * @param {WebSocketsConnectExplorerAliasWSClientOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {WebSocketsConnectExplorerAliasWS} Switching Protocols; subsequent frames follow x-websocket.
   *
   * @example
   * ```ts
   * const connection = client.webSockets.connectExplorerAlias();
   *
   * try {
   *   for await (const message of connection) {
   *     console.log(message);
   *   }
   * } finally {
   *   connection.close();
   * }
   * ```
   */
  connectExplorerAlias(
    options?: WebSocketsConnectExplorerAliasWSClientOptions,
  ): WebSocketsConnectExplorerAliasWS {
    return new WebSocketsConnectExplorerAliasWS(this._client, {}, options);
  }
}
