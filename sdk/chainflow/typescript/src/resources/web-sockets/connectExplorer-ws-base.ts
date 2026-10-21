// File generated from our OpenAPI spec by Scalar. See README.md for details.

import {
  WebSocketsConnectExplorerEmitter,
  WebSocketsConnectExplorerStreamMessage,
  WebSocketError,
  buildURL,
  parameterHeaders,
} from './connectExplorer-ws-internal-base';
import { InternalEventEmitter } from '../../core/EventEmitter';
import { sleep } from '../../internal/utils/sleep';
import { type WebSocketLike, ReadyState } from '../../internal/ws-adapter';
import {
  SendQueue,
  flattenRawData,
  isRecoverableClose,
  type RawWebSocketData,
  type ReconnectingEvent,
  type ReconnectingOverrides,
  type UnsentMessage,
} from '../../internal/ws';
import * as WebSocketsAPI from './web-sockets';
import { ChainflowDataGateway } from '../../client';
import { ChainflowDataGatewayError } from '../../error';

export interface WebSocketsConnectExplorerWSParameters extends Record<string, unknown> {}

export interface WebSocketsConnectExplorerWSReconnectOptions {
  /** Called before each reconnect attempt. */
  onReconnecting(
    event: ReconnectingEvent<WebSocketsConnectExplorerWSParameters>,
  ): ReconnectingOverrides<WebSocketsConnectExplorerWSParameters> | void;
  /** Maximum number of reconnection attempts. Default: 5. Set to 0 to disable reconnection. */
  maxRetries?: number;
  /** Initial backoff delay in milliseconds. Default: 500. */
  initialDelay?: number;
  /** Maximum backoff delay in milliseconds. Default: 8000. */
  maxDelay?: number;
}

export interface WebSocketsConnectExplorerWSBaseOptions {
  /** Options for automatic reconnection on recoverable close codes. */
  reconnect?: WebSocketsConnectExplorerWSReconnectOptions | null | undefined;
  /** Maximum size of the outgoing message queue in bytes. Default: 1 MB. */
  maxQueueSize?: number | undefined;
}

export abstract class WebSocketsConnectExplorerWSBase<
  TSocket extends WebSocketLike,
> extends WebSocketsConnectExplorerEmitter {
  url!: URL;
  socket!: TSocket;

  protected _client: ChainflowDataGateway;
  protected _parameters: WebSocketsConnectExplorerWSParameters | null | undefined;
  private _reconnectOptions: WebSocketsConnectExplorerWSReconnectOptions | null;
  private _sendQueue: SendQueue<unknown>;
  private _isReconnecting = false;
  private _intentionallyClosed = false;
  private _closeCode = 1000;
  private _closeReason = 'OK';
  private _lastCloseCode = 1006;
  private _lastCloseReason = '';
  private _internalEvents = new InternalEventEmitter<{
    socketSwap: (oldSocket: TSocket, newSocket: TSocket) => void;
    reconnecting: (event: ReconnectingEvent<WebSocketsConnectExplorerWSParameters>) => void;
    reconnected: () => void;
    close: (code: number, reason: string, unsent: UnsentMessage<unknown>[]) => void;
  }>();

  constructor(
    client: ChainflowDataGateway,
    parameters: WebSocketsConnectExplorerWSParameters,
    options?: WebSocketsConnectExplorerWSBaseOptions | undefined,
  ) {
    super();
    this._client = client;
    this._parameters = parameters ?? undefined;
    this._reconnectOptions = options?.reconnect ?? null;
    this._sendQueue = new SendQueue<unknown>(options?.maxQueueSize);
  }

  protected _connectInitial(): void {
    this.url = buildURL(this._client, this._parameters ?? {});
    this.socket = this._connect();
  }

  protected abstract _createSocket(url: URL, authHeaders: Record<string, string>): TSocket;

  send(event: unknown): void {
    if (this._isReconnecting || this.socket.readyState === ReadyState.CONNECTING) {
      if (!this._sendQueue.enqueue(event))
        this._onError(null, 'send queue is full, message discarded', undefined);
      return;
    }
    if (this.socket.readyState !== ReadyState.OPEN) {
      this._onError(null, 'cannot send on a closed WebSocket', undefined);
      return;
    }
    try {
      this.socket.send(JSON.stringify(event));
    } catch (err) {
      this._onError(null, 'could not send data', err);
    }
  }

  sendRaw(data: RawWebSocketData): void {
    if (this._isReconnecting || this.socket.readyState === ReadyState.CONNECTING) {
      if (!this._sendQueue.enqueueRaw(data))
        this._onError(null, 'send queue is full, message discarded', undefined);
      return;
    }
    if (this.socket.readyState !== ReadyState.OPEN) {
      this._onError(null, 'cannot send on a closed WebSocket', undefined);
      return;
    }
    try {
      this.socket.send(flattenRawData(data));
    } catch (err) {
      this._onError(null, 'could not send data', err);
    }
  }

  close(props?: { code: number; reason: string }): void {
    this._intentionallyClosed = true;
    this._closeCode = props?.code ?? 1000;
    this._closeReason = props?.reason ?? 'OK';
    try {
      this.socket.close(this._closeCode, this._closeReason);
    } catch (err) {
      this._onError(null, 'could not close the connection', err);
    }
  }

  stream(): AsyncIterableIterator<WebSocketsConnectExplorerStreamMessage> {
    return this[Symbol.asyncIterator]();
  }

  [Symbol.asyncIterator](): AsyncIterableIterator<WebSocketsConnectExplorerStreamMessage> {
    const queue: WebSocketsConnectExplorerStreamMessage[] = [];
    const resolvers: (() => void)[] = [];
    let done = false;
    let currentSocket = this.socket;
    const push = (msg: WebSocketsConnectExplorerStreamMessage) => {
      queue.push(msg);
      resolvers.shift()?.();
    };
    const flushResolvers = () => {
      for (let resolver = resolvers.shift(); resolver; resolver = resolvers.shift()) resolver();
    };
    const cleanup = () => {
      this.off('event', onEvent);
      this.off('raw', onRaw);
      this.off('error', onEmitterError);
      currentSocket.off('open', onOpen);
      this._internalEvents.off('close', onClose);
      this._internalEvents.off('socketSwap', onSocketSwap);
      this._internalEvents.off('reconnecting', onReconnecting);
      this._internalEvents.off('reconnected', onReconnected);
    };
    const onEvent = (event: unknown) => {
      if (!isErrorEvent(event)) push({ type: 'message', message: event as never });
    };
    const onRaw = (data: RawWebSocketData) => push({ type: 'raw', data });
    const onEmitterError = (error: WebSocketError) => push({ type: 'error', error });
    const onOpen = () => push({ type: 'open' });
    const onReconnecting = (event: ReconnectingEvent<WebSocketsConnectExplorerWSParameters>) =>
      push({ type: 'reconnecting', reconnect: event });
    const onReconnected = () => push({ type: 'reconnected' });
    const onClose = (code: number, reason: string, unsent: UnsentMessage<unknown>[]) => {
      push({ type: 'close', code, reason, unsent });
      done = true;
      flushResolvers();
      cleanup();
    };
    const onSocketSwap = (oldSocket: TSocket, newSocket: TSocket) => {
      oldSocket.off('open', onOpen);
      newSocket.on('open', onOpen);
      currentSocket = newSocket;
    };
    this.on('event', onEvent);
    this.on('raw', onRaw);
    this.on('error', onEmitterError);
    this.socket.on('open', onOpen);
    this._internalEvents.on('close', onClose);
    this._internalEvents.on('socketSwap', onSocketSwap);
    this._internalEvents.on('reconnecting', onReconnecting);
    this._internalEvents.on('reconnected', onReconnected);
    if (this._isReconnecting)
      push({
        type: 'reconnecting',
        reconnect: { attempt: 0, maxAttempts: 0, delay: 0, closeCode: 0, parameters: undefined },
      });
    else if (this.socket.readyState === ReadyState.CONNECTING) push({ type: 'connecting' });
    else if (this.socket.readyState === ReadyState.OPEN) push({ type: 'open' });
    else if (this.socket.readyState === ReadyState.CLOSING) push({ type: 'closing' });
    else {
      push({
        type: 'close',
        code: this._lastCloseCode,
        reason: this._lastCloseReason,
        unsent: this._sendQueue.drain(),
      });
      done = true;
      cleanup();
    }
    const next = (): Promise<IteratorResult<WebSocketsConnectExplorerStreamMessage>> =>
      new Promise((resolve) => {
        if (queue.length > 0) resolve({ value: queue.shift()!, done: false });
        else if (done) resolve({ value: undefined, done: true });
        else
          resolvers.push(() => {
            if (queue.length > 0) resolve({ value: queue.shift()!, done: false });
            else resolve({ value: undefined, done: true });
          });
      });
    return {
      next,
      return: () => {
        done = true;
        cleanup();
        flushResolvers();
        return Promise.resolve({ value: undefined, done: true });
      },
      [Symbol.asyncIterator]() {
        return this;
      },
    };
  }

  private _connect(): TSocket {
    this.url = buildURL(this._client, this._parameters ?? {});
    const socket = this._createSocket(this.url, this._authHeaders());
    socket.on('message', (data: string | ArrayBuffer | ArrayBufferView, isBinary: boolean) => {
      if (isBinary) {
        this._emit('raw', data);
        return;
      }
      const text = typeof data === 'string' ? data : String(data);
      let event: unknown;
      try {
        event = JSON.parse(text);
      } catch {
        this._emit('raw', data);
        return;
      }
      this._emit('event', event as never);
      if (isErrorEvent(event)) this._onError(event as never);
      else emitTypedEvent(this, event);
    });
    socket.on('error', (err: Error) => {
      if (!this._isReconnecting) this._onError(null, err.message, err);
    });
    socket.on('open', () => this._flushSendQueue());
    socket.on('close', (code: number, reason: string) => {
      if (socket !== this.socket) return;
      if (!this._intentionallyClosed && this._canReconnect(code)) this._reconnect(code);
      else if (!this._isReconnecting) this._emitPermanentClose(code, reason);
    });
    return socket;
  }

  private _canReconnect(code: number): boolean {
    if (
      this._intentionallyClosed ||
      !this._reconnectOptions ||
      this._reconnectOptions.maxRetries === 0 ||
      !this._reconnectOptions.onReconnecting
    )
      return false;
    return isRecoverableClose(code);
  }

  private async _reconnect(closeCode: number): Promise<void> {
    if (this._isReconnecting || !this._reconnectOptions) return;
    this._isReconnecting = true;
    const maxRetries = this._reconnectOptions.maxRetries ?? 5;
    const initialDelay = this._reconnectOptions.initialDelay ?? 500;
    const maxDelay = this._reconnectOptions.maxDelay ?? 8000;
    for (let attempt = 1; attempt <= maxRetries; attempt++) {
      if (!this._canReconnect(closeCode)) {
        this._isReconnecting = false;
        this._emitPermanentClose(
          this._intentionallyClosed ? this._closeCode : closeCode,
          this._intentionallyClosed ? this._closeReason : 'reconnect aborted',
        );
        return;
      }
      const delay = Math.round(
        Math.min(initialDelay * 2 ** (attempt - 1), maxDelay) * (0.75 + Math.random() * 0.25),
      );
      let reconnectingEvent: ReconnectingEvent<WebSocketsConnectExplorerWSParameters> = {
        attempt,
        maxAttempts: maxRetries,
        delay,
        closeCode,
        parameters: this._parameters ? { ...this._parameters } : undefined,
      };
      let overrides: ReconnectingOverrides<WebSocketsConnectExplorerWSParameters> | void;
      try {
        overrides = this._reconnectOptions.onReconnecting(reconnectingEvent);
      } catch (err) {
        this._isReconnecting = false;
        this._onError(null, 'onReconnecting callback threw', err);
        this._emitPermanentClose(closeCode, 'onReconnecting callback threw');
        return;
      }
      if (overrides && 'abort' in overrides && overrides.abort) {
        this._isReconnecting = false;
        this._emitPermanentClose(closeCode, 'reconnect aborted by handler');
        return;
      }
      if (overrides && 'parameters' in overrides) {
        this._parameters = overrides.parameters;
        reconnectingEvent = { ...reconnectingEvent, parameters: this._parameters };
      }
      this._emit('reconnecting', reconnectingEvent);
      this._internalEvents._emit('reconnecting', reconnectingEvent);
      if (!this._canReconnect(closeCode)) {
        this._isReconnecting = false;
        this._emitPermanentClose(
          this._intentionallyClosed ? this._closeCode : closeCode,
          this._intentionallyClosed ? this._closeReason : 'reconnect aborted',
        );
        return;
      }
      await sleep(delay);
      if (!this._canReconnect(closeCode)) {
        this._isReconnecting = false;
        this._emitPermanentClose(
          this._intentionallyClosed ? this._closeCode : closeCode,
          this._intentionallyClosed ? this._closeReason : 'reconnect aborted',
        );
        return;
      }
      let closeCodePromise: Promise<number> | undefined;
      try {
        const oldSocket = this.socket;
        this.socket = this._connect();
        closeCodePromise = new Promise<number>((resolve) => {
          this.socket.once('close', resolve);
        });
        await this._awaitOpen(this.socket);
        this._internalEvents._emit('socketSwap', oldSocket, this.socket);
        this._isReconnecting = false;
        this._flushSendQueue();
        this._emit('reconnected');
        this._internalEvents._emit('reconnected');
        return;
      } catch {
        if (closeCodePromise) closeCode = await closeCodePromise;
      }
    }
    this._isReconnecting = false;
    this._onError(
      null,
      `WebSocket reconnect failed after ${maxRetries} attempts (close code: ${closeCode})`,
      undefined,
    );
    this._emitPermanentClose(closeCode, `reconnect failed after ${maxRetries} attempts`);
  }

  private _awaitOpen(socket: TSocket): Promise<void> {
    return new Promise((resolve, reject) => {
      const cleanup = () => {
        socket.off('open', onOpen);
        socket.off('error', onError);
        socket.off('close', onFail);
      };
      const onOpen = () => {
        cleanup();
        resolve();
      };
      const onError = (err: Error) => {
        cleanup();
        reject(err);
      };
      const onFail = () => {
        cleanup();
        reject(new Error('socket closed before open'));
      };
      socket.once('open', onOpen);
      socket.once('error', onError);
      socket.once('close', onFail);
    });
  }

  private _flushSendQueue(): void {
    try {
      this._sendQueue.flush((data) => this.socket.send(flattenRawData(data)));
    } catch (err) {
      this._onError(null, 'could not send queued data', err);
    }
  }

  private _emitPermanentClose(code: number, reason: string): void {
    this._lastCloseCode = code;
    this._lastCloseReason = reason;
    const unsent = this._sendQueue.drain();
    this._internalEvents._emit('close', code, reason, unsent);
    this._emit('close', code, reason, unsent);
  }

  protected _authHeaders(): Record<string, string> {
    return { ...this._client.webSocketAuthHeaders(), ...parameterHeaders(this._parameters ?? {}) };
  }
}

const isErrorEvent = (event: unknown): boolean =>
  typeof event === 'object' && event !== null && 'type' in event && event.type === 'error';

const emitTypedEvent = (emitter: WebSocketsConnectExplorerEmitter, event: unknown): void => {
  if (typeof event === 'object' && event !== null && 'type' in event && typeof event.type === 'string') {
    (emitter._emit as (eventName: string, payload: unknown) => void)(event.type, event);
  }
};
