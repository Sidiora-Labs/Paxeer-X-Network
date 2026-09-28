export const PROVIDER_SOURCE = String.raw`
(() => {
  if (globalThis.__PAXPORT_PROVIDER_INSTALLED__) return;
  Object.defineProperty(globalThis, '__PAXPORT_PROVIDER_INSTALLED__', {
    value: true,
    configurable: false,
    enumerable: false,
    writable: false,
  });

  const listeners = new Map();
  let requestSequence = 0;

  class PaxPortProvider {
    constructor() {
      Object.defineProperties(this, {
        isPaxPort: { value: true, enumerable: true },
        isConnected: { value: () => true, enumerable: false },
      });
    }

    async request(args) {
      if (!args || typeof args !== 'object' || Array.isArray(args)) {
        throw Object.assign(new Error('Invalid provider request.'), { code: -32600 });
      }
      const method = args.method;
      if (typeof method !== 'string' || method.length < 2 || method.length > 64) {
        throw Object.assign(new Error('Invalid provider method.'), { code: -32600 });
      }
      const params = args.params === undefined ? [] : args.params;
      try {
        return await globalThis.__paxportRemoteRpc({
          id: String(++requestSequence),
          method,
          params,
        });
      } catch (caught) {
        const source = caught && typeof caught === 'object' ? caught : {};
        const error = new Error(
          typeof source.message === 'string' ? source.message : 'Wallet request failed.',
        );
        error.code = typeof source.code === 'number' ? source.code : -32603;
        if ('data' in source) error.data = source.data;
        throw error;
      }
    }

    on(event, listener) {
      if (typeof event !== 'string' || typeof listener !== 'function') return this;
      const current = listeners.get(event) || new Set();
      current.add(listener);
      listeners.set(event, current);
      return this;
    }

    removeListener(event, listener) {
      listeners.get(event)?.delete(listener);
      return this;
    }

    emit(event, payload) {
      for (const listener of listeners.get(event) || []) {
        try {
          listener(payload);
        } catch {
        }
      }
    }

    enable() {
      return this.request({ method: 'eth_requestAccounts' });
    }

    send(methodOrPayload, paramsOrCallback) {
      if (typeof methodOrPayload === 'string') {
        return this.request({ method: methodOrPayload, params: paramsOrCallback });
      }
      const payload = methodOrPayload;
      const callback = typeof paramsOrCallback === 'function' ? paramsOrCallback : null;
      const operation = this.request({
        method: payload?.method,
        params: payload?.params,
      }).then(result => ({
        id: payload?.id,
        jsonrpc: payload?.jsonrpc || '2.0',
        result,
      }));
      if (callback) {
        operation.then(value => callback(null, value), error => callback(error));
        return;
      }
      return operation;
    }

    sendAsync(payload, callback) {
      this.send(payload, callback);
    }
  }

  const provider = Object.freeze(new PaxPortProvider());
  const info = Object.freeze({
    uuid: '4b2f8b28-f3c1-4a8f-a214-2e3134f04139',
    name: 'PaxPort',
    icon: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="16" fill="%23050505"/><path d="M18 14h18c9 0 15 5 15 13s-6 14-15 14H28v9H18V14zm10 9v9h7c4 0 6-2 6-5s-2-4-6-4h-7z" fill="%2396d19f"/></svg>',
    rdns: 'com.paxeer.wallet',
  });

  const announce = () => {
    globalThis.dispatchEvent(new CustomEvent('eip6963:announceProvider', {
      detail: Object.freeze({ info, provider }),
    }));
  };

  Object.defineProperty(globalThis, 'ethereum', {
    value: provider,
    configurable: false,
    enumerable: true,
    writable: false,
  });
  Object.defineProperty(globalThis, '__PAXPORT_PROVIDER_EMIT__', {
    value: (event, payload) => provider.emit(event, payload),
    configurable: false,
    enumerable: false,
    writable: false,
  });
  globalThis.addEventListener('eip6963:requestProvider', announce);
  queueMicrotask(announce);
})();
`;
