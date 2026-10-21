// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { APIResource } from '../resource';
import { APIPromise } from '../api-promise';
import type { RequestOptions } from '../internal/request-options';
import { buildHeaders } from '../internal/headers';
import type * as PriceFeedsAPI from './price-feeds';
import type * as TradingViewAPI from './trading-view';

export class Gateway extends APIResource {
  /**
   * Checks Nginx only; does not probe the auth helper, database or market services.
   *
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<GatewayListHealthResponse>} Gateway is responding.
   *
   * @example
   * ```ts
   * const response = await client.gateway.listHealth();
   * ```
   */
  listHealth(options?: RequestOptions): APIPromise<GatewayListHealthResponse> {
    return this._client.get('/health', {
      ...options,
      headers: buildHeaders([{ Accept: 'text/plain' }, options?.headers]),
    });
  }

  /**
   * Authenticate with the separate WS API key, not the REST key. Lifetime is 300 seconds. Response contains a secret; never log or cache it. Credentials are only checked at upgrade; an established socket is not closed solely because the token expires. Body must contain only symbol.
   *
   * @param {GatewayCreateWebSocketTokenParams} body - The request body to send.
   * @param {RequestOptions} [options] - Options to apply to the request, such as headers and an abort signal.
   * @returns {APIPromise<WsToken>} Opaque credential for the requested stream scope.
   *
   * @example
   * ```ts
   * const wsToken = await client.gateway.createWebSocketToken({ symbol: 'btc' });
   * ```
   */
  createWebSocketToken(
    body: GatewayCreateWebSocketTokenParams,
    options?: RequestOptions,
  ): APIPromise<WsToken> {
    return this._client.post('/auth/ws-token', { body, ...options });
  }
}

export interface WsToken {
  /**
   * Opaque signed credential; treat as secret. Not an OAuth token or a JWT.
   */
  token: string;
  symbol: WsScope;
  /**
   * @format date-time
   */
  expiresAt: IsoTimestamp;
  expiresIn: 300;
}

export type WsScope =
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
  | 'spx500'
  | 'markets'
  | 'explorer';

export type IsoTimestamp = string;

export type GatewayListHealthResponse = string;

export interface GatewayCreateWebSocketTokenParams {
  symbol: WsScope;
}
export declare namespace Gateway {
  export {
    type WsToken as WsToken,
    type WsScope as WsScope,
    type IsoTimestamp as IsoTimestamp,
    type GatewayListHealthResponse as GatewayListHealthResponse,
    type GatewayCreateWebSocketTokenParams as GatewayCreateWebSocketTokenParams,
  };
}
