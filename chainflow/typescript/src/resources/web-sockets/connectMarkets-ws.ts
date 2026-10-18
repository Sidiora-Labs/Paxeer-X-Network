// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { WebSocket, type ClientOptions } from 'ws';
import { NodeWebSocket } from '../../internal/ws-adapter-node';
import {
  WebSocketsConnectMarketsWSBase,
  type WebSocketsConnectMarketsWSBaseOptions,
  type WebSocketsConnectMarketsWSParameters,
} from './connectMarkets-ws-base';
import { ChainflowDataGateway } from '../../client';

export type {
  WebSocketsConnectMarketsWSParameters,
  WebSocketsConnectMarketsWSReconnectOptions,
} from './connectMarkets-ws-base';

export interface WebSocketsConnectMarketsWSClientOptions
  extends ClientOptions,
    WebSocketsConnectMarketsWSBaseOptions {}

export class WebSocketsConnectMarketsWS extends WebSocketsConnectMarketsWSBase<NodeWebSocket> {
  private _wsOptions: ClientOptions | null | undefined;

  constructor(
    client: ChainflowDataGateway,
    parameters: WebSocketsConnectMarketsWSParameters,
    options?: WebSocketsConnectMarketsWSClientOptions | null | undefined,
  ) {
    if (!WebSocket) {
      throw new Error('WebSocketsConnectMarketsWS requires the "ws" package but it could not be loaded.');
    }

    const { reconnect, maxQueueSize, ...wsOptions } = options ?? {};
    super(client, parameters, { reconnect, maxQueueSize });
    this._wsOptions = wsOptions;
    this._connectInitial();
  }

  protected _createSocket(url: URL, authHeaders: Record<string, string>): NodeWebSocket {
    const ws = new WebSocket(url, {
      ...this._wsOptions,
      headers: {
        ...authHeaders,
        ...this._wsOptions?.headers,
      },
    });
    return new NodeWebSocket(ws);
  }
}
