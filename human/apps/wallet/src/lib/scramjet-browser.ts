const PROXY_PREFIX = '/api/proxy';

interface ProviderRequest {
  method: string;
  params?: unknown[] | Record<string, unknown>;
  origin: string;
}

interface ScramjetBrowserOptions {
  iframe: HTMLIFrameElement;
  onUrlChange: (url: string) => void;
  onProviderRequest: (request: ProviderRequest) => Promise<unknown>;
}

interface RuntimeFrame {
  go: (url: string) => void;
  back: () => void;
  forward: () => void;
  reload: () => void;
}

interface RuntimeController {
  wait: () => Promise<void>;
  createFrame: (
    iframe: HTMLIFrameElement,
    options: { plugins: unknown[] },
  ) => RuntimeFrame;
}

interface RuntimePlugin {
  tap: (
    hook: unknown,
    callback: (context: RuntimeFrameContext) => void,
  ) => void;
}

interface RuntimeFrameContext {
  window: Window & typeof globalThis;
  isTopLevel: boolean;
}

interface RuntimePluginConstructor {
  new (...args: unknown[]): RuntimePlugin;
}

interface ScramjetGlobals extends Window {
  $scramjetController?: {
    Controller: new (options: Record<string, unknown>) => RuntimeController;
  };
  $scramjetUtils?: {
    ManagedPlugin: RuntimePluginConstructor;
    UrlWatcherPlugin: new (callback: (url: string) => void) => unknown;
  };
  LibcurlTransport?: {
    LibcurlClient: new (options: { wisp: string }) => unknown;
  };
  EpoxyTransport?: {
    default: new (options: { wisp: string }) => unknown;
  };
}

type ProviderListener = (...args: unknown[]) => void;

interface InjectedProvider {
  chainId: string;
  networkVersion: string;
  selectedAddress: string | null;
  isPaxport: true;
  isMetaMask: true;
  providers: InjectedProvider[];
  request: (request: {
    method: string;
    params?: unknown[] | Record<string, unknown>;
  }) => Promise<unknown>;
  enable: () => Promise<unknown>;
  send: (...args: unknown[]) => unknown;
  sendAsync: (
    payload: { id?: string | number; method: string; params?: unknown[] },
    callback: (error: unknown, response?: unknown) => void,
  ) => void;
  on: (event: string, listener: ProviderListener) => InjectedProvider;
  removeListener: (event: string, listener: ProviderListener) => InjectedProvider;
  emit: (event: string, payload?: unknown) => void;
}

let runtimePromise: Promise<ScramjetGlobals> | null = null;

function useMobileSafariTransport(): boolean {
  return (
    /iPad|iPhone|iPod/.test(navigator.userAgent) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
  );
}

function loadScript(path: string): Promise<void> {
  const existing = document.querySelector<HTMLScriptElement>(
    `script[data-paxport-proxy="${path}"]`,
  );
  if (existing?.dataset.loaded === 'true') return Promise.resolve();

  return new Promise((resolve, reject) => {
    const script = existing ?? document.createElement('script');
    const onLoad = () => {
      script.dataset.loaded = 'true';
      resolve();
    };
    const onError = () => reject(new Error(`Failed to load ${path}`));
    script.addEventListener('load', onLoad, { once: true });
    script.addEventListener('error', onError, { once: true });
    if (!existing) {
      script.src = path;
      script.dataset.paxportProxy = path;
      document.head.appendChild(script);
    }
  });
}

async function proxyServiceWorker(): Promise<ServiceWorker> {
  const registration = await navigator.serviceWorker.register(
    `${PROXY_PREFIX}/sw.js`,
    {
      scope: `${PROXY_PREFIX}/`,
      type: 'classic',
      updateViaCache: 'none',
    },
  );

  if (registration.active) return registration.active;

  const worker = registration.installing ?? registration.waiting;
  if (!worker) throw new Error('The proxy service worker did not start.');
  await new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(
      () => reject(new Error('The proxy service worker timed out.')),
      15_000,
    );
    worker.addEventListener('statechange', () => {
      if (worker.state === 'activated') {
        window.clearTimeout(timer);
        resolve();
      }
      if (worker.state === 'redundant') {
        window.clearTimeout(timer);
        reject(new Error('The proxy service worker was rejected.'));
      }
    });
  });
  if (!registration.active) {
    throw new Error('The proxy service worker is unavailable.');
  }
  return registration.active;
}

async function loadRuntime(): Promise<ScramjetGlobals> {
  if (runtimePromise) return runtimePromise;
  runtimePromise = (async () => {
    await proxyServiceWorker();
    await loadScript(`${PROXY_PREFIX}/scram/scramjet.js`);
    await loadScript(`${PROXY_PREFIX}/controller/controller.api.js`);
    await loadScript(`${PROXY_PREFIX}/scram/scramjet-utils.js`);
    const mobileSafari = useMobileSafariTransport();
    await loadScript(
      mobileSafari
        ? `${PROXY_PREFIX}/clients/epoxy-client.js`
        : `${PROXY_PREFIX}/clients/libcurl-client.js`,
    );
    const runtime = window as ScramjetGlobals;
    if (
      !runtime.$scramjetController ||
      !runtime.$scramjetUtils ||
      (mobileSafari
        ? typeof runtime.EpoxyTransport?.default !== 'function'
        : !runtime.LibcurlTransport)
    ) {
      throw new Error('The proxy browser runtime is incomplete.');
    }
    return runtime;
  })().catch((error) => {
    runtimePromise = null;
    throw error;
  });
  return runtimePromise;
}

function createProvider(
  target: Window & typeof globalThis,
  origin: () => string,
  requestHandler: (request: ProviderRequest) => Promise<unknown>,
): InjectedProvider {
  const listeners = new Map<string, Set<ProviderListener>>();
  const provider: InjectedProvider = {
    chainId: '0x7d',
    networkVersion: '125',
    selectedAddress: null,
    isPaxport: true,
    isMetaMask: true,
    providers: [] as InjectedProvider[],
    request: async ({
      method,
      params,
    }: {
      method: string;
      params?: unknown[] | Record<string, unknown>;
    }) => requestHandler({ method, params, origin: origin() }),
    enable: () => requestHandler({
      method: 'eth_requestAccounts',
      origin: origin(),
    }),
    send(...args: unknown[]) {
      if (typeof args[0] === 'string') {
        return provider.request({
          method: args[0],
          params: Array.isArray(args[1]) ? args[1] : undefined,
        });
      }
      const payload = args[0] as {
        id?: string | number;
        method?: string;
        params?: unknown[];
      };
      const callback = args[1];
      if (typeof callback === 'function' && typeof payload?.method === 'string') {
        provider.sendAsync(
          { ...payload, method: payload.method },
          callback as (error: unknown, response?: unknown) => void,
        );
        return;
      }
      if (typeof payload?.method === 'string') {
        return provider.request({
          method: payload.method,
          params: payload.params,
        });
      }
      throw new Error('Invalid provider request.');
    },
    sendAsync(
      payload: { id?: string | number; method: string; params?: unknown[] },
      callback: (error: unknown, response?: unknown) => void,
    ) {
      provider.request(payload).then(
        (result) => callback(null, {
          jsonrpc: '2.0',
          id: payload.id ?? null,
          result,
        }),
        (error) => callback(error),
      );
    },
    on(event: string, listener: ProviderListener) {
      const handlers = listeners.get(event) ?? new Set<ProviderListener>();
      handlers.add(listener);
      listeners.set(event, handlers);
      return provider;
    },
    removeListener(event: string, listener: ProviderListener) {
      listeners.get(event)?.delete(listener);
      return provider;
    },
    emit(event: string, payload?: unknown) {
      if (event === 'accountsChanged') {
        const accounts = Array.isArray(payload) ? payload : [];
        provider.selectedAddress =
          typeof accounts[0] === 'string' ? accounts[0] : null;
      }
      if (event === 'chainChanged' && typeof payload === 'string') {
        provider.chainId = payload;
      }
      for (const listener of listeners.get(event) ?? []) {
        listener(payload);
      }
    },
  };
  provider.providers = [provider];

  Object.defineProperty(target, 'ethereum', {
    configurable: true,
    enumerable: true,
    value: provider,
    writable: false,
  });

  const info = Object.freeze({
    uuid: '1b7c2f40-66ad-4fca-92c6-0b25cae9f125',
    name: 'Paxport',
    icon: `${location.origin}/icons/android/launchericon-192x192.png`,
    rdns: 'com.paxeer.paxport',
  });
  const announce = () => {
    target.dispatchEvent(
      new target.CustomEvent('eip6963:announceProvider', {
        detail: Object.freeze({ info, provider }),
      }),
    );
  };
  target.addEventListener('eip6963:requestProvider', announce);
  target.setTimeout(announce, 0);
  return provider;
}

export interface ScramjetBrowserSession {
  go: (url: string) => void;
  back: () => void;
  forward: () => void;
  reload: () => void;
  emitProviderEvent: (event: string, payload?: unknown) => void;
  destroy: () => void;
}

export async function createScramjetBrowser(
  options: ScramjetBrowserOptions,
): Promise<ScramjetBrowserSession> {
  if (!('serviceWorker' in navigator)) {
    throw new Error('This browser does not support the proxy runtime.');
  }

  const runtime = await loadRuntime();
  const workerRegistration = await navigator.serviceWorker.getRegistration(
    `${location.origin}${PROXY_PREFIX}/`,
  );
  const worker = workerRegistration?.active;
  if (!worker) throw new Error('The proxy service worker is unavailable.');

  const wispProtocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const transportOptions = {
    wisp: `${wispProtocol}//${location.host}${PROXY_PREFIX}/wisp/`,
  };
  const transport = useMobileSafariTransport()
    ? new runtime.EpoxyTransport!.default(transportOptions)
    : new runtime.LibcurlTransport!.LibcurlClient(transportOptions);
  const controller = new runtime.$scramjetController!.Controller({
    serviceworker: worker,
    transport,
    config: {
      prefix: `${PROXY_PREFIX}/session/`,
      scramjetPath: `${PROXY_PREFIX}/scram/scramjet.js`,
      injectPath: `${PROXY_PREFIX}/controller/controller.inject.js`,
      wasmPath: `${PROXY_PREFIX}/scram/scramjet.wasm`,
    },
  });
  await controller.wait();

  const providers = new Set<InjectedProvider>();
  let activeOrigin: string | null = null;
  const setActiveOrigin = (value: string) => {
    try {
      const parsed = new URL(value);
      activeOrigin =
        parsed.protocol === 'https:' &&
        !parsed.username &&
        !parsed.password
          ? parsed.origin
          : null;
    } catch {
      activeOrigin = null;
    }
  };
  const handleUrlChange = (value: string) => {
    setActiveOrigin(value);
    options.onUrlChange(value);
  };
  const requireActiveOrigin = () => {
    if (!activeOrigin) {
      throw new Error('The active dApp origin is unavailable.');
    }
    return activeOrigin;
  };
  const ManagedPlugin = runtime.$scramjetUtils!.ManagedPlugin;
  class PaxportProviderPlugin extends ManagedPlugin {
    constructor() {
      super('paxport-provider', []);
    }

    install(frame: {
      hooks: { init: { pre: unknown } };
    }) {
      this.tap(frame.hooks.init.pre, (context) => {
        if (!context.isTopLevel) return;
        const provider = createProvider(
          context.window,
          requireActiveOrigin,
          options.onProviderRequest,
        );
        providers.add(provider);
      });
    }
  }

  const urlWatcher = new runtime.$scramjetUtils!.UrlWatcherPlugin(
    handleUrlChange,
  );
  const frame = controller.createFrame(options.iframe, {
    plugins: [new PaxportProviderPlugin(), urlWatcher],
  });

  return {
    go: (url) => {
      setActiveOrigin(url);
      frame.go(url);
    },
    back: () => frame.back(),
    forward: () => frame.forward(),
    reload: () => frame.reload(),
    emitProviderEvent: (event, payload) => {
      for (const provider of providers) provider.emit(event, payload);
    },
    destroy: () => {
      providers.clear();
      options.iframe.src = 'about:blank';
    },
  };
}
