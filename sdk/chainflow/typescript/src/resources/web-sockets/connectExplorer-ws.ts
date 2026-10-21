// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { WebSocket, type ClientOptions } from 'ws';
import { NodeWebSocket } from '../../internal/ws-adapter-node';
import {
  WebSocketsConnectExplorerWSBase,
  type WebSocketsConnectExplorerWSBaseOptions,
  type WebSocketsConnectExplorerWSParameters,
} from './connectExplorer-ws-base';
import { ChainflowDataGateway } from '../../client';

export type {
  WebSocketsConnectExplorerWSParameters,
  WebSocketsConnectExplorerWSReconnectOptions,
} from './connectExplorer-ws-base';

export interface WebSocketsConnectExplorerWSClientOptions
  extends ClientOptions,
    WebSocketsConnectExplorerWSBaseOptions {}

export class WebSocketsConnectExplorerWS extends WebSocketsConnectExplorerWSBase<NodeWebSocket> {
  private _wsOptions: ClientOptions | null | undefined;

  constructor(
    client: ChainflowDataGateway,
    parameters: WebSocketsConnectExplorerWSParameters,
    options?: WebSocketsConnectExplorerWSClientOptions | null | undefined,
  ) {
    if (!WebSocket) {
      throw new Error('WebSocketsConnectExplorerWS requires the "ws" package but it could not be loaded.');
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
