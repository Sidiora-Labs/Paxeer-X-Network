// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { BrowserWebSocket } from '../../internal/ws-adapter-browser';
import {
  WebSocketsConnectMarketsWSBase,
  type WebSocketsConnectMarketsWSBaseOptions,
  type WebSocketsConnectMarketsWSParameters,
} from './connectMarkets-ws-base';
import { ChainflowDataGateway } from '../../client';

export type {
  WebSocketsConnectMarketsWSParameters,
  WebSocketsConnectMarketsWSReconnectOptions,
  WebSocketsConnectMarketsWSBaseOptions,
} from './connectMarkets-ws-base';

export interface WebSocketsConnectMarketsWSBrowserOptions extends WebSocketsConnectMarketsWSBaseOptions {
  /** WebSocket sub-protocols to pass to the browser WebSocket constructor. */
  protocols?: string | string[];
}

// Minimal type declaration for the browser WebSocket constructor.
declare const WebSocket: {
  new (url: string, protocols?: string | string[]): any;
};

export class WebSocketsConnectMarketsWS extends WebSocketsConnectMarketsWSBase<BrowserWebSocket> {
  private _protocols: string | string[] | undefined;

  constructor(
    client: ChainflowDataGateway,
    parameters: WebSocketsConnectMarketsWSParameters,
    options?: WebSocketsConnectMarketsWSBrowserOptions | null | undefined,
  ) {
    if (typeof (globalThis as any).WebSocket === 'undefined') {
      throw new Error(
        'WebSocketsConnectMarketsWS requires a browser environment with native WebSocket support.',
      );
    }

    const { reconnect, maxQueueSize, protocols } = options ?? {};
    super(client, parameters, { reconnect, maxQueueSize });
    this._protocols = protocols;
    this._connectInitial();
  }

  protected _createSocket(url: URL, _authHeaders: Record<string, string>): BrowserWebSocket {
    // Browser WebSocket does not support custom headers; auth headers are ignored.
    const ws = new WebSocket(url.toString(), this._protocols);
    return new BrowserWebSocket(ws);
  }
}
