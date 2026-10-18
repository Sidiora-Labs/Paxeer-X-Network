import EventEmitter from "eventemitter3";
import { messageHandlers } from "./handler/handler";

export type NetworkId = "testnet" | "mainnet";

export type WSOptions = {
  privateUrl: string;
  publicUrl?: string;
  networkId?: NetworkId;
  accountId?: string;

  onSigntureRequest?: (accountId: string) => Promise<any>;
};

export type unsubscribe = () => void;

export enum WebSocketEvent {
  OPEN = "open",
  CLOSE = "close",
  ERROR = "error",
  MESSAGE = "message",
  CONNECTING = "connecting",
  RECONNECTING = "reconnecting",
}

export type MessageParams = {
  event: string;
  topic: string;

  onMessage?: (message: any) => any;

  params?: any;
  // [key: string]: any;
};

type WSMessageHandler = {
  onMessage: (message: any) => void;
  onError?: (error: any) => void;
  onClose?: (event: any) => void;
  onUnsubscribe: (event: any) => any;
  formatter?: (message: any) => any;
};

type Topics = {
  params: MessageParams;
  isPrivate?: boolean;
  isOnce?: boolean;
  callback: WSMessageHandler[];
};

const defaultMessageFormatter = (message: any) => message.data;
const COMMON_ID = "OqdphuyCtYWxwzhxyLLjOWNdFP7sQt8RPWzmb5xY";

const TIME_OUT = 1000 * 60 * 2;
const CONNECT_LIMIT = 5;

export class WS extends EventEmitter {
  private _publicSocket!: WebSocket;
  private privateSocket?: WebSocket;

  private _eventContainer: Map<string, Set<(message: any) => void>> = new Map();

  private publicIsReconnecting: boolean = false;
  private privateIsReconnecting: boolean = false;

  private reconnectInterval: number = 1000;

  private authenticated: boolean = false;

  private _pendingPrivateSubscribe: any[][] = [];
  private _pendingPublicSubscribe: any[][] = [];

  // all message handlers
  private _eventHandlers: Map<string, Topics> = new Map();
  private _eventPrivateHandlers: Map<string, Topics> = new Map();

  private _publicHeartbeatTime?: number;
  private _privateHeartbeatTime?: number;

  private _publicRetryCount: number = 0;
  private _privateRetryCount: number = 0;

  constructor(private options: WSOptions) {
    super();
    this.createPublicSC(options);

    if (!!options.accountId) {
      this.createPrivateSC(options);
    }

    this.bindEvents();
  }

  private bindEvents() {
    if (typeof document !== "undefined") {
      document.addEventListener?.(
        "visibilitychange",
        this.onVisibilityChange.bind(this),
      );
    }

    if (typeof window !== "undefined") {
      window.addEventListener?.(
        "online",
        this.onNetworkStatusChange.bind(this),
      );
      // window.addEventListener?.("offline", (e) => {
      //   // console.log("👀👀👀👀👀👀👀👀👀", "offline");
      //   this.emit("offline", e);
      // });
    }
  }

  private onVisibilityChange() {
    if (document.visibilityState === "visible") {
      this.checkSocketStatus();
    }
    // else {
    //   this.publicSocket.close();
    //   this.privateSocket?.close();
    // }
  }

  private onNetworkStatusChange() {
    if (navigator.onLine) {
      this.checkSocketStatus();
    }
  }

  /**
   * Determine the current connection status,
   * 1. If it is disconnected, reconnect
   * 2. If no message is received for too long, disconnect and reconnect actively
   * 3. When returning from the background and the network status changes, the following process is followed
   */
  private checkSocketStatus() {
    const now = Date.now();

    //
    //   "👀👀 checkNetworkStatus 👀👀",
    //   this._publicHeartbeatTime,
    //   this._privateHeartbeatTime,
    //   now,
    //   this.publicSocket.readyState,
    //   this.privateSocket?.readyState
    // );

    // check the last time
    // If the view is not visible, do not process it
    if (document.visibilityState !== "visible") return;
    // If the network is not available, do not process it
    if (!navigator.onLine) return;

    // public
    if (!this.publicIsReconnecting) {
      if (this._publicSocket?.readyState === WebSocket.CLOSED) {
        this.reconnectPublic();
      } else {
        if (now - this._publicHeartbeatTime! > TIME_OUT) {
          //unsubscribe all public topic
          this._publicSocket?.close(3888);
        }
      }
    }

    if (!this.privateIsReconnecting) {
      // private
      if (this.privateSocket?.readyState === WebSocket.CLOSED) {
        this.reconnectPrivate();
      } else {
        if (
          this._privateHeartbeatTime &&
          now - this._privateHeartbeatTime! > TIME_OUT
        ) {
          // unsubscribe all private topic
          this.privateSocket?.close(3888);
        }
      }
    }
  }

  public openPrivate(accountId: string) {
    if (this.privateSocket?.readyState === WebSocket.OPEN) {
      return;
    }
    this.createPrivateSC({
      ...this.options,
      accountId,
    });
  }

  public closePrivate(code?: number, reason?: string) {
    if (this.privateSocket?.readyState !== WebSocket.OPEN) {
      return;
    }

    this.authenticated = false;

    this._pendingPrivateSubscribe = [];

    this._eventPrivateHandlers.clear();

    this.privateSocket?.close(code, reason);
  }

  private createPublicSC(options: WSOptions) {
    if (!options.publicUrl) {
      return;
    }

    if (
      this._publicSocket &&
      this._publicSocket.readyState === WebSocket.OPEN
    ) {
      return;
    }

    this._publicSocket = new WebSocket(
      `${options.publicUrl}/ws/stream/${COMMON_ID}`,
    );
    this._publicSocket.onopen = this.onOpen.bind(this);
    // this.publicSocket.onmessage = this.onPublicMessage.bind(this);
    this._publicSocket.addEventListener(
      "message",
      this.onPublicMessage.bind(this),
    );
    this._publicSocket.addEventListener("close", this.onPublicClose.bind(this));
    this._publicSocket.addEventListener("error", this.onPublicError.bind(this));
    // this.publicSocket.onclose = this.onPublicClose.bind(this);
    // this.publicSocket.onerror = this.onPublicError.bind(this);
  }

  private createPrivateSC(options: WSOptions) {
    if (
      this.privateSocket &&
      this.privateSocket.readyState === WebSocket.OPEN
    ) {
      return;
    }

    this.options = options;

    this.privateSocket = new WebSocket(
      `${this.options.privateUrl}/v2/ws/private/stream/${options.accountId}`,
    );
    this.privateSocket.onopen = this.onPrivateOpen.bind(this);
    this.privateSocket.onmessage = this.onPrivateMessage.bind(this);
    this.privateSocket.onclose = this.onPrivateClose.bind(this);
    this.privateSocket.onerror = this.onPrivateError.bind(this);
  }

  private onOpen(event: Event) {
    //
    if (this._pendingPublicSubscribe.length > 0) {
      this._pendingPublicSubscribe.forEach(([params, cb, isOnce]) => {
        this.subscribe(params, cb, isOnce);
      });
      this._pendingPublicSubscribe = [];
    }

    this.publicIsReconnecting = false;

    this.emit("status:change", {
      type: WebSocketEvent.OPEN,
      isPrivate: false,
      isReconnect: this._publicRetryCount > 0,
    });

    this._publicRetryCount = 0;
  }

  private onPrivateOpen(event: Event) {
    // auth
    this.authenticate(this.options.accountId!)
      .then(() => {
        this.privateIsReconnecting = false;

        this.emit("status:change", {
          type: WebSocketEvent.OPEN,
          isPrivate: true,
          isReconnect: this._privateRetryCount > 0,
        });

        this._privateRetryCount = 0;
      })
      .catch((e) => {
        console.log("ws authenticate failed", e);
      });
  }

  private onMessage(
    event: MessageEvent,
    socket: WebSocket,
    handlerMap: Map<string, Topics>,
  ) {
    try {
      const message = JSON.parse(event.data);

      const commoneHandler = messageHandlers.get(message.event);

      if (message.event === "auth" && message.success) {
        this.authenticated = true;
        this.handlePendingPrivateTopic();
        return;
      }

      if (commoneHandler) {
        commoneHandler.handle(message, socket);
      } else {
        const topicKey = this.getTopicKeyFromMessage(message);

        const eventhandler = handlerMap.get(topicKey);
        //
        if (eventhandler?.callback) {
          // console.log("👀👀👀👀👀👀👀👀👀", topicKey, eventhandler?.callback);
          eventhandler.callback.forEach((cb) => {
            const data = cb.formatter
              ? cb.formatter(message)
              : defaultMessageFormatter(message);

            if (data) {
              cb.onMessage(data);
            }
          });
        }

        // emit event
        this._eventContainer.forEach((_, key) => {
          const reg = new RegExp(key);
          if (reg.test(topicKey)) {
            this.emit(key, message);
          }
        });
      }

      //
    } catch (e) {}

    // You can process the received message here
  }

  private onPublicMessage(event: MessageEvent) {
    this.onMessage(event, this._publicSocket, this._eventHandlers);
    // update last message time for public
    this._publicHeartbeatTime = Date.now();
  }

  private onPrivateMessage(event: MessageEvent) {
    this.onMessage(event, this.privateSocket!, this._eventPrivateHandlers);
    // update last message time for private
    this._privateHeartbeatTime = Date.now();
  }

  private handlePendingPrivateTopic() {
    if (this._pendingPrivateSubscribe.length > 0) {
      this._pendingPrivateSubscribe.forEach(([params, cb]) => {
        this.privateSubscribe(params, cb);
      });
      this._pendingPrivateSubscribe = [];
    }
  }

  private onPublicClose(event: CloseEvent) {
    // move handler to pending
    this._eventHandlers.forEach((value, key) => {
      value.callback.forEach((cb) => {
        this._pendingPublicSubscribe.push([value.params, cb, value.isOnce]);
      });

      this._eventHandlers.delete(key);
    });

    this.emit("status:change", {
      type: WebSocketEvent.CLOSE,
      isPrivate: false,
    });

    setTimeout(() => this.checkSocketStatus(), 0);
  }

  private onPrivateClose(event: CloseEvent) {
    if (event.code === 3887) return;
    if (this.privateIsReconnecting) return;
    this._eventPrivateHandlers.forEach((value, key) => {
      value.callback.forEach((cb) => {
        this._pendingPrivateSubscribe.push([value.params, cb, value.isOnce]);
      });

      this._eventPrivateHandlers.delete(key);
    });
    this.authenticated = false;

    this.emit("status:change", {
      type: WebSocketEvent.CLOSE,
      isPrivate: true,
      event,
    });

    setTimeout(() => this.checkSocketStatus(), 0);
  }

  private onPublicError(event: Event) {
    console.error("public WebSocket error:", event);
    this.publicIsReconnecting = false;

    if (this._publicSocket?.readyState === WebSocket.OPEN) {
      this._publicSocket?.close(3888);
    } else {
      // retry connect
      if (this._publicRetryCount > CONNECT_LIMIT) return;
      setTimeout(() => {
        // this.createPublicSC(this.options);
        this.reconnectPublic();
        // this._publicRetryCount++;
      }, this._publicRetryCount * 1000);
    }

    this.errorBoardscast(event, this._eventHandlers);
    this.emit("status:change", {
      type: WebSocketEvent.ERROR,
      isPrivate: false,
    });
  }

  private onPrivateError(event: Event) {
    console.error("Private WebSocket error:", event);
    this.privateIsReconnecting = false;

    if (this.privateSocket?.readyState === WebSocket.OPEN) {
      this.privateSocket.close(3888);
    } else {
      // retry connect
      if (this._privateRetryCount > CONNECT_LIMIT) return;
      setTimeout(() => {
        // this.createPublicSC(this.options);
        this.reconnectPrivate();
        // this._privateRetryCount++;
      }, this._privateRetryCount * 1000);
    }

    this.errorBoardscast(event, this._eventPrivateHandlers);
    this.emit("status:change", { type: WebSocketEvent.ERROR, isPrivate: true });
  }

  private errorBoardscast(error: any, eventHandlers: Map<string, Topics>) {
    eventHandlers.forEach((value) => {
      value.callback.forEach((cb) => {
        cb.onError?.(error);
      });
    });
  }

  send = (message: any) => {
    if (typeof message !== "string") {
      message = JSON.stringify(message);
    }
    if (typeof message === "undefined") return;
    if (this._publicSocket?.readyState === WebSocket.OPEN) {
      this._publicSocket?.send(message);
      //
    } else {
      console.warn("WebSocket connection is not open. Cannot send message.");
    }
  };

  close() {
    this._publicSocket?.close();
    this.privateSocket?.close();
  }

  set accountId(accountId: string) {}

  private async authenticate(accountId: string) {
    if (this.authenticated) return;
    if (!this.privateSocket) {
      console.error("private ws not connected");
      return;
    }

    if (this.privateSocket.readyState !== WebSocket.OPEN) {
      return;
    }

    const message = await this.options.onSigntureRequest?.(accountId);

    this.privateSocket.send(
      JSON.stringify({
        id: "auth",
        event: "auth",
        params: {
          paxeerNetwork_key: message.publicKey,
          sign: message.signature,
          timestamp: message.timestamp,
        },
      }),
    );
    // this.wsSubject.next({ type: "authenticate" });
    // this.authenticated = true;
  }

  privateSubscribe(
    params: any,
    callback: WSMessageHandler | Omit<WSMessageHandler, "onUnsubscribe">,
  ) {
    const [subscribeMessage, onUnsubscribe] = this.generateMessage(
      params,
      (callback as WSMessageHandler).onUnsubscribe,
    );

    if (this.privateSocket?.readyState !== WebSocket.OPEN) {
      this._pendingPrivateSubscribe.push([params, callback]);
      return () => {
        this.unsubscribePrivate(subscribeMessage);
      };
    }

    const topic = subscribeMessage.topic || subscribeMessage.event;

    const handler = this._eventPrivateHandlers.get(topic);
    const callbacks = {
      ...callback,
      onUnsubscribe,
    };

    if (!handler) {
      this._eventPrivateHandlers.set(topic, {
        params,
        callback: [callbacks],
      });
      this.privateSocket.send(JSON.stringify(subscribeMessage));
    } else {
      handler.callback.push(callbacks);
    }

    return () => {
      this.unsubscribePrivate(subscribeMessage);
    };
  }

  subscribe(
    params: any,
    callback: WSMessageHandler | Omit<WSMessageHandler, "onUnsubscribe">,
    once?: boolean,
    id?: string,
  ): unsubscribe | undefined {
    if (!this._publicSocket) {
      return;
    }

    const [subscribeMessage, onUnsubscribe] = this.generateMessage(
      params,
      (callback as WSMessageHandler).onUnsubscribe,
      (callback as WSMessageHandler).onMessage,
    );

    //

    if (this._publicSocket.readyState !== WebSocket.OPEN) {
      this._pendingPublicSubscribe.push([params, callback, once, id]);

      if (!once) {
        return () => {
          this.unsubscribePublic(subscribeMessage);
        };
      }
      return;
    }

    const topic = this.getTopicKeyFromParams(subscribeMessage);
    // const topic = subscribeMessage.topic || subscribeMessage.event;

    const handler = this._eventHandlers.get(topic);
    const callbacks = {
      ...callback,
      onUnsubscribe,
    };

    if (!handler) {
      this._eventHandlers.set(topic, {
        params,
        isOnce: once,
        callback: [callbacks],
      });
      this._publicSocket.send(JSON.stringify(subscribeMessage));
    } else {
      if (once) {
        handler.callback = [callbacks];
        this._publicSocket.send(JSON.stringify(subscribeMessage));
      } else {
        handler.callback.push(callbacks);
      }
    }

    // this._subscriptionPublicTopics.push({params, cb: [cb]});

    if (!once) {
      return () => {
        this.unsubscribePublic(subscribeMessage);
      };
    }
  }

  private getTopicKeyFromParams(params: any): string {
    let topic;

    if (params.topic) {
      topic = params.topic;
    } else {
      const eventName = params.event;
      topic = params.event;

      if (params.id) {
        topic += `_${params.id}`;
      }
    }

    return topic;
  }

  private getTopicKeyFromMessage(message: any): string {
    let topic;
    if (message.topic) {
      topic = message.topic;
    } else {
      if (message.event) {
        topic = `${message.event}`;

        if (message.id) {
          topic += `_${message.id}`;
        }
      }
    }

    return topic;
  }

  // sendPublicMessage(){
  //   if(this.publicSocket.readyState !== )
  // }

  onceSubscribe(
    params: any,
    callback: Omit<WSMessageHandler, "onUnsubscribe">,
  ) {
    this.subscribe(params, callback, true);
  }

  private unsubscribe(
    parmas: MessageParams,
    webSocket: WebSocket,
    handlerMap: Map<string, Topics>,
  ) {
    const topic = parmas.topic || parmas.event;
    const handler = handlerMap.get(topic);

    // console.log("unsubscribe", topic);

    if (!!handler && Array.isArray(handler?.callback)) {
      if (handler!.callback.length === 1) {
        const unsubscribeMessage = handler!.callback[0].onUnsubscribe(topic);

        //
        webSocket.send(JSON.stringify(unsubscribeMessage));
        handlerMap.delete(topic);
        //post unsubscribe message
      } else {
        const index = handler.callback.findIndex(
          (cb) => cb.onMessage === parmas.onMessage,
        );

        // console.log(index, handler.callback.length);

        if (index === -1) return;

        handler.callback.splice(index, 1);

        // handlerMap.set(topic, {
        //   ...handler,
        //   callback: handler.callback.splice(index, 1),
        // });
      }
    }
  }

  private unsubscribePrivate(parmas: MessageParams) {
    this.unsubscribe(parmas, this.privateSocket!, this._eventPrivateHandlers);
  }

  private unsubscribePublic(parmas: MessageParams) {
    this.unsubscribe(parmas, this._publicSocket, this._eventHandlers);
  }

  private generateMessage(
    params: any,
    onUnsubscribe?: (event: string) => any,
    onMessage?: (message: any) => any,
  ): [MessageParams, (event: string) => any] {
    let subscribeMessage;

    if (typeof params === "string") {
      subscribeMessage = { event: "subscribe", topic: params };
    } else {
      subscribeMessage = params;
    }

    if (typeof onUnsubscribe !== "function") {
      if (typeof params === "string") {
        onUnsubscribe = () => ({ event: "unsubscribe", topic: params });
      } else {
        onUnsubscribe = () => ({ event: "unsubscribe", topic: params.topic });
      }
    }

    return [{ ...subscribeMessage, onMessage }, onUnsubscribe];
  }

  private reconnectPublic() {
    if (this.publicIsReconnecting) return;
    this.publicIsReconnecting = true;

    if (typeof window === "undefined") return;
    window.setTimeout(() => {
      this._publicRetryCount++;
      this.createPublicSC(this.options);
      // this.emit("reconnect:public", { count: this._publicRetryCount });
      this.emit("status:change", {
        type: WebSocketEvent.RECONNECTING,
        isPrivate: false,
        count: this._publicRetryCount,
      });
    }, this.reconnectInterval);
  }

  private reconnectPrivate() {
    if (!this.options.accountId) return;
    if (this.privateIsReconnecting) return;
    this.privateIsReconnecting = true;

    if (typeof window === "undefined") return;
    window.setTimeout(() => {
      this._privateRetryCount++;
      this.createPrivateSC(this.options);

      this.emit("status:change", {
        type: WebSocketEvent.RECONNECTING,
        isPrivate: true,
        count: this._privateRetryCount,
      });
    }, this.reconnectInterval);
  }

  // get publicSocket(): WebSocket {
  //   return this._publicSocket;
  // }

  get client(): {
    public: WebSocket;
    private?: WebSocket;
  } {
    return {
      public: this._publicSocket,
      private: this.privateSocket,
    };
  }
}
