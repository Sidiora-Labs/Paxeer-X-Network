type Eip1193RequestArgs = {
  method: string;
  params?: unknown[] | object;
};

type ProviderEventName = "accountsChanged" | "chainChanged" | "connect" | "disconnect" | "message";

type ProviderListener = (...args: unknown[]) => void;

type PendingRequest = {
  resolve: (value: unknown) => void;
  reject: (reason?: unknown) => void;
};

type BridgeRequestMessage = {
  type: "SIDIORA_EIP1193_REQUEST";
  id: string;
  method: string;
  params?: unknown[] | object;
};

type BridgeResponseMessage = {
  type: "SIDIORA_EIP1193_RESPONSE";
  id: string;
  result?: unknown;
  error?: { code?: number; message?: string; data?: unknown };
};

type BridgeEventMessage = {
  type: "SIDIORA_EIP1193_EVENT";
  event: ProviderEventName;
  payload?: unknown;
};

type BridgeInboundMessage = BridgeResponseMessage | BridgeEventMessage;

type BridgeMessage = BridgeRequestMessage | BridgeInboundMessage;

function isBridgeMessage(data: unknown): data is BridgeInboundMessage {
  if (!data || typeof data !== "object") return false;
  const candidate = data as { type?: unknown };
  return candidate.type === "SIDIORA_EIP1193_RESPONSE" || candidate.type === "SIDIORA_EIP1193_EVENT";
}

function createProviderError(error?: { code?: number; message?: string; data?: unknown }): Error & { code?: number; data?: unknown } {
  const providerError = new Error(error?.message || "EIP-1193 request failed") as Error & { code?: number; data?: unknown };
  providerError.code = error?.code;
  providerError.data = error?.data;
  return providerError;
}

export class PostMessageEip1193Provider {
  private readonly targetOrigin: string;
  private readonly pending = new Map<string, PendingRequest>();
  private readonly listeners = new Map<ProviderEventName, Set<ProviderListener>>();
  private readonly handleMessageBound: (event: MessageEvent<unknown>) => void;
  readonly isSidioraPostMessageProvider = true;

  constructor(targetOrigin: string) {
    this.targetOrigin = targetOrigin;
    this.handleMessageBound = this.handleMessage.bind(this);
  }

  mount() {
    window.addEventListener("message", this.handleMessageBound);
  }

  unmount() {
    window.removeEventListener("message", this.handleMessageBound);
    for (const [, request] of this.pending) {
      request.reject(new Error("PostMessage EIP-1193 provider unmounted"));
    }
    this.pending.clear();
  }

  request(args: Eip1193RequestArgs): Promise<unknown> {
    if (!window.parent || window.parent === window) {
      return Promise.reject(new Error("PostMessage EIP-1193 provider requires iframe context"));
    }

    const id = `${Date.now()}-${Math.random().toString(16).slice(2)}`;
    const message: BridgeRequestMessage = {
      type: "SIDIORA_EIP1193_REQUEST",
      id,
      method: args.method,
      params: args.params,
    };

    return new Promise((resolve, reject) => {
      this.pending.set(id, { resolve, reject });
      window.parent.postMessage(message, this.targetOrigin);
    });
  }

  on(event: string, listener: ProviderListener): void {
    const listeners = this.listeners.get(event as ProviderEventName) ?? new Set<ProviderListener>();
    listeners.add(listener);
    this.listeners.set(event as ProviderEventName, listeners);
  }

  removeListener(event: string, listener: ProviderListener): void {
    this.listeners.get(event as ProviderEventName)?.delete(listener);
  }

  emit(event: ProviderEventName, payload?: unknown) {
    const listeners = this.listeners.get(event);
    if (!listeners) return;
    for (const listener of listeners) {
      if (Array.isArray(payload)) {
        listener(...payload);
      } else if (payload === undefined) {
        listener();
      } else {
        listener(payload);
      }
    }
  }

  private handleMessage(event: MessageEvent<unknown>) {
    if (event.origin !== this.targetOrigin) return;
    if (!isBridgeMessage(event.data)) return;

    if (event.data.type === "SIDIORA_EIP1193_RESPONSE") {
      const pendingRequest = this.pending.get(event.data.id);
      if (!pendingRequest) return;
      this.pending.delete(event.data.id);
      if (event.data.error) {
        pendingRequest.reject(createProviderError(event.data.error));
        return;
      }
      pendingRequest.resolve(event.data.result);
      return;
    }

    this.emit(event.data.event, event.data.payload);
  }
}

export function installPostMessageEip1193Provider() {
  if (typeof window === "undefined") return null;
  if (window.parent === window) return null;

  const targetOrigin = process.env.NEXT_PUBLIC_WALLET_IFRAME_ORIGIN;
  if (!targetOrigin) return null;

  const existingProvider = window.ethereum as (Window["ethereum"] & { isSidioraPostMessageProvider?: boolean; unmount?: () => void }) | undefined;
  if (existingProvider?.isSidioraPostMessageProvider) {
    return existingProvider as PostMessageEip1193Provider;
  }

  const provider = new PostMessageEip1193Provider(targetOrigin);
  provider.mount();
  window.ethereum = provider as Window["ethereum"];

  window.dispatchEvent(new Event("ethereum#initialized"));
  return provider;
}
