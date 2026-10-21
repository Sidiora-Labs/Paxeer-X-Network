// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { WebSocket, type ClientOptions } from 'ws';
import { NodeWebSocket } from '../../internal/ws-adapter-node';
import {
  WebSocketsConnectPriceFeedWSBase,
  type WebSocketsConnectPriceFeedWSBaseOptions,
  type WebSocketsConnectPriceFeedWSParameters,
} from './connectPriceFeed-ws-base';
import { ChainflowDataGateway } from '../../client';

export type {
  WebSocketsConnectPriceFeedWSParameters,
  WebSocketsConnectPriceFeedWSReconnectOptions,
} from './connectPriceFeed-ws-base';

export interface WebSocketsConnectPriceFeedWSClientOptions
  extends ClientOptions,
    WebSocketsConnectPriceFeedWSBaseOptions {}

export class WebSocketsConnectPriceFeedWS extends WebSocketsConnectPriceFeedWSBase<NodeWebSocket> {
  private _wsOptions: ClientOptions | null | undefined;

  constructor(
    client: ChainflowDataGateway,
    parameters: WebSocketsConnectPriceFeedWSParameters,
    options?: WebSocketsConnectPriceFeedWSClientOptions | null | undefined,
  ) {
    if (!WebSocket) {
      throw new Error('WebSocketsConnectPriceFeedWS requires the "ws" package but it could not be loaded.');
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
