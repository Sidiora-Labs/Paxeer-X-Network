// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { WebSocket, type ClientOptions } from 'ws';
import { NodeWebSocket } from '../../internal/ws-adapter-node';
import {
  WebSocketsConnectExplorerAliasWSBase,
  type WebSocketsConnectExplorerAliasWSBaseOptions,
  type WebSocketsConnectExplorerAliasWSParameters,
} from './connectExplorerAlias-ws-base';
import { ChainflowDataGateway } from '../../client';

export type {
  WebSocketsConnectExplorerAliasWSParameters,
  WebSocketsConnectExplorerAliasWSReconnectOptions,
} from './connectExplorerAlias-ws-base';

export interface WebSocketsConnectExplorerAliasWSClientOptions
  extends ClientOptions,
    WebSocketsConnectExplorerAliasWSBaseOptions {}

export class WebSocketsConnectExplorerAliasWS extends WebSocketsConnectExplorerAliasWSBase<NodeWebSocket> {
  private _wsOptions: ClientOptions | null | undefined;

  constructor(
    client: ChainflowDataGateway,
    parameters: WebSocketsConnectExplorerAliasWSParameters,
    options?: WebSocketsConnectExplorerAliasWSClientOptions | null | undefined,
  ) {
    if (!WebSocket) {
      throw new Error(
        'WebSocketsConnectExplorerAliasWS requires the "ws" package but it could not be loaded.',
      );
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
