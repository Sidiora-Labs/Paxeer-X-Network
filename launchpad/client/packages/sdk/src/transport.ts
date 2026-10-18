import { decodeContract, errorsText } from './validate.js';
import type { ContractTypes } from './types.gen.js';
import type { ContractTypes as V2ContractTypes } from '@kindle/sdk-v2/types';
import { v2ContractValidators } from './validators-v2.gen.js';

export function decodeV2Contract<N extends keyof V2ContractTypes>(name: N, value: unknown): V2ContractTypes[N] {
  const validator = Object.hasOwn(v2ContractValidators, name) ? v2ContractValidators[name] : undefined;
  if (!validator) throw new Error(`Unknown v2 contract ${name}`);
  if (!validator(value)) throw new TypeError(`Invalid v2 contract ${name}: ${errorsText(validator.errors)}`);
  return value as V2ContractTypes[N];
}
export type { V2ContractTypes };
export interface RequestOptions { signal?: AbortSignal; method?: 'GET'|'POST'|'PATCH'|'PUT'|'DELETE'; body?: unknown; rawBody?: Blob; binaryResponse?: boolean; authenticated?: boolean; idempotentRead?: boolean; service?: 'api'|'identity'|'media'; timeoutMs?: number; headers?: Readonly<Record<string,string>>; cookieSession?: boolean; requestId?: string }

export class CapabilityUnavailableError extends Error { readonly code = 'capability_unavailable'; constructor(message = 'Capability unavailable', readonly status = 501) { super(message); } }
export class HttpError extends Error {
  readonly code: string;
  constructor(readonly status: number, readonly envelope?: V2ContractTypes['Error'], code?: string) {
    super(`HTTP ${status}${envelope ? ': ' + envelope.code : code ? ': ' + code : ''}`);
    this.code = envelope?.code ?? code ?? 'TRANSPORT_ERROR';
  }
}
export class StreamTransportError extends Error {
  constructor(readonly code: string, readonly status?: number) { super(code); }
}
export interface TransportConfig {
  apiOrigin: string; identityOrigin: string; mediaOrigin: string; streamOrigin: string;
  approvedStreamOrigins: readonly string[]; production: boolean;
  session?: { mode: 'bearer'; token: () => string | undefined } | { mode: 'cookie'; applicationOrigin: string; csrfToken: () => string };
}
function origin(value: string, production: boolean, websocket = false): URL {
  const url = new URL(value);
  const secure = websocket ? 'wss:' : 'https:', insecure = websocket ? 'ws:' : 'http:';
  if (url.protocol !== secure && (production || url.protocol !== insecure)) throw new TypeError('Insecure transport origin');
  if (url.username || url.password || url.search || url.hash || url.pathname !== '/') throw new TypeError('Expected credential-free origin');
  const host = url.hostname.toLowerCase();
  if (production && (!host.includes('.') || host.endsWith('.internal') || host.endsWith('.local') || host.endsWith('.localhost') || host === 'localhost' || /^\d+(?:\.\d+){3}$/.test(host) || host.includes(':') || /(?:^|\.)(?:redis|nats|broker|postgres|rabbitmq)(?:\.|$)/.test(host))) throw new TypeError('Private service origin forbidden');
  return url;
}
export function validateTransport(config: TransportConfig): TransportConfig {
  for (const key of ['apiOrigin','identityOrigin','mediaOrigin'] as const) origin(config[key], config.production);
  const stream = origin(config.streamOrigin, config.production, true);
  const allow = config.approvedStreamOrigins.map(value => origin(value, config.production, true).origin);
  if (!allow.includes(stream.origin)) throw new TypeError('Unapproved WebSocket origin');
  if (config.session?.mode === 'cookie' && origin(config.session.applicationOrigin,config.production).origin !== origin(config.apiOrigin,config.production).origin) throw new TypeError('Session cookies require same origin');
  return config;
}
function abortableDelay(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve,reject) => {
    if (signal?.aborted) { reject(signal.reason); return; }
    const abort = () => { clearTimeout(timer); signal?.removeEventListener('abort',abort); reject(signal?.reason); };
    const timer = setTimeout(() => { signal?.removeEventListener('abort',abort); resolve(); },ms);
    signal?.addEventListener('abort',abort,{once:true});
  });
}
export class SdkClient {
  readonly config: TransportConfig;
  // The default resolves `globalThis.fetch` at call time and invokes it as a
  // plain function. In browsers `fetch` is a Window operation: storing the
  // bare global and calling it as `this.fetcher(...)` passes the SdkClient as
  // the receiver and throws "Illegal invocation" before any request is sent.
  constructor(config: TransportConfig, private readonly fetcher: typeof fetch = (input, init) => globalThis.fetch(input, init)) {
    const checked = validateTransport(config);
    // Copy so later caller mutation cannot bypass startup origin validation.
    this.config = Object.freeze({ ...checked, approvedStreamOrigins: Object.freeze([...checked.approvedStreamOrigins]), ...(checked.session ? { session: Object.freeze({...checked.session}) } : {}) });
  }
  async request<N extends keyof ContractTypes>(path: string, response: N, options: RequestOptions = {}): Promise<ContractTypes[N]> {
    return decodeContract(response, await this.boundedRequest(path, options, 1));
  }
  async requestV2<N extends keyof V2ContractTypes>(path: string, response: N, options: RequestOptions = {}): Promise<V2ContractTypes[N]> {
    return decodeV2Contract(response, await this.boundedRequest(path, options, 2));
  }
  async requestV2Empty(path: string, options: RequestOptions): Promise<void> { const value = await this.boundedRequest(path, options, 2); if (value !== undefined) throw new TypeError('Expected empty operation response'); }
  async requestV1Generated<N extends keyof V2ContractTypes>(path: string, response: N, options: RequestOptions = {}): Promise<V2ContractTypes[N]> { return decodeV2Contract(response, await this.boundedRequest(path, options, 1)); }
  async requestV3<N extends keyof V2ContractTypes>(path: string, response: N, options: RequestOptions = {}): Promise<V2ContractTypes[N]> { return decodeV2Contract(response, await this.boundedRequest(path, options, 3)); }
  async requestGeneratedBinary(path: string, options: RequestOptions, major: 1|2|3): Promise<Response> { const value = await this.boundedRequest(path, { ...options, binaryResponse: true }, major); if (!(value instanceof Response)) throw new TypeError('Expected binary operation response'); return value; }
  private async boundedRequest(path: string, options: RequestOptions, major: 1|2|3): Promise<unknown> {
    const timeout = options.timeoutMs ?? 30000;
    if (!Number.isSafeInteger(timeout) || timeout < 1 || timeout > 60000) throw new RangeError('Invalid request timeout');
    const controller = new AbortController();
    const abort = () => controller.abort(options.signal?.reason);
    options.signal?.addEventListener('abort', abort, { once: true });
    if (options.signal?.aborted) abort();
    const timer = setTimeout(() => controller.abort(new DOMException('Request timed out', 'TimeoutError')), timeout);
    try { return await this.requestValue(path, { ...options, signal: controller.signal }, major); }
    finally { clearTimeout(timer); options.signal?.removeEventListener('abort', abort); }
  }
  private async requestValue(path: string, options: RequestOptions, major: 1|2|3): Promise<unknown> {
    if (path.length > 8192 || !(major === 1 ? /^\/v1\/[A-Za-z0-9_/?&=.%,-]+$/.test(path) : (major === 2 ? /^\/v2\/[A-Za-z0-9_/:?&=.%+,~-]+$/.test(path) : /^\/v3\/[A-Za-z0-9_/:?&=.%+,~-]+$/.test(path))) || /(?:token|ticket|jwt|authorization)=/i.test(path)) throw new TypeError('Invalid API path or URL credential');
    const service = options.service ?? (major === 1 ? 'api' : (/^\/v[12]\/(?:auth(?:\/|$)|stream(?:-tickets|\/tickets))/.test(path) ? 'identity' : /^\/v[12]\/(?:uploads|assets|media)(?:\/|$)/.test(path) ? 'media' : 'api'));
    const base = service === 'identity' ? this.config.identityOrigin : service === 'media' ? this.config.mediaOrigin : this.config.apiOrigin;
    const url = new URL(path,base);
    if (url.origin !== new URL(base).origin) throw new TypeError('Cross-origin request');
    for (const key of url.searchParams.keys()) {
      if (/^(?:token|ticket|jwt|authorization|access_token|refresh_token)$/i.test(key)) throw new TypeError('URL credential forbidden');
    }
    if ((options.authenticated || options.cookieSession) && this.config.session?.mode === 'cookie' && url.origin !== new URL(this.config.session.applicationOrigin).origin) throw new TypeError('Session cookies require same-origin destination');
    const method = options.method ?? 'GET';
    const headers: Record<string,string> = { Accept: 'application/json' };
    for (const [key, value] of Object.entries(options.headers ?? {})) {
      if (!/^(?:Idempotency-Key|X-Action-(?:Wallet|Signature|Nonce|Expires|Audience)|If-Match)$/i.test(key) || /[\r\n]/.test(value)) throw new TypeError('Unsupported operation header');
      headers[key] = value;
    }
    if (options.requestId !== undefined) { if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(options.requestId)) throw new TypeError('Invalid request identity'); headers['X-Request-ID'] = options.requestId; }
    if (options.rawBody !== undefined) { if (options.body !== undefined || !(options.rawBody instanceof Blob) || options.rawBody.size < 1 || options.rawBody.size > 6291456 || !['image/png','image/jpeg','image/webp'].includes(options.rawBody.type)) throw new TypeError('Invalid binary asset body'); headers['Content-Type'] = options.rawBody.type; }
    else if (options.body !== undefined) headers['Content-Type'] = 'application/json';
    if (options.authenticated && this.config.session?.mode === 'bearer') {
      const token = this.config.session.token();
      if (!token || /[\r\n]/.test(token)) throw new TypeError('Access token required');
      headers.Authorization = `Bearer ${token}`;
    } else if (options.authenticated && !this.config.session) throw new TypeError('Session required');
    if (options.authenticated && method !== 'GET' && this.config.session?.mode === 'cookie') {
      const csrf = this.config.session.csrfToken();
      if (!csrf || /[\r\n]/.test(csrf)) throw new TypeError('CSRF token required');
      headers['X-CSRF-Token'] = csrf;
    }
    const credentials: RequestCredentials = (options.authenticated || options.cookieSession) && this.config.session?.mode === 'cookie' ? 'same-origin' : 'omit';
    for (let attempt=0;;attempt++) {
      options.signal?.throwIfAborted();
      let result: Response;
      // Detach before calling so an injected native fetch never sees the client as its receiver.
      const fetcher = this.fetcher;
      try { result = await fetcher(url,{ method, headers, credentials, redirect:'manual', ...(options.signal ? {signal:options.signal} : {}), ...(options.rawBody !== undefined ? {body: options.rawBody} : options.body === undefined ? {} : {body:JSON.stringify(options.body)}) }); }
      catch (error) {
        options.signal?.throwIfAborted();
        if (method !== 'GET' || !options.idempotentRead || attempt >= 2) throw new StreamTransportError('NETWORK_INTERRUPTED');
        await abortableDelay(Math.random()*Math.min(30000,500*2**attempt),options.signal); continue;
      }
      options.signal?.throwIfAborted();
      // Redirects are never followed. A server that answered with one did
      // answer, so it is an HTTP outcome with its status (0 for the browser's
      // opaque redirect), not a network interruption.
      if (result.type === 'opaqueredirect' || (result.status >= 300 && result.status < 400)) { await result.body?.cancel(); throw new HttpError(result.status, undefined, 'UNEXPECTED_REDIRECT'); }
      let envelope: V2ContractTypes['Error'] | undefined;
      if (!result.ok && major !== 1) {
        try { const decoded = decodeV2Contract('Error', await result.clone().json()); envelope = { code: decoded.code, message: decoded.code, requestId: decoded.requestId, retryable: decoded.retryable, retryAfterMs: decoded.retryAfterMs, fieldErrors: decoded.fieldErrors, details: null }; } catch { options.signal?.throwIfAborted(); }
      }
      if (result.status === 501 || (result.status === 404 && !envelope)) { await result.body?.cancel(); throw new CapabilityUnavailableError(`Unavailable endpoint ${path.split('?')[0]}`, result.status); }
      if (result.status === 503 && envelope?.retryable !== false && method === 'GET' && options.idempotentRead && attempt < 2) {
        const retry = result.headers.get('Retry-After');
        const seconds = retry === null ? NaN : Number(retry);
        const wait = Number.isFinite(seconds) ? seconds*1000 : retry ? Date.parse(retry)-Date.now() : 0;
        await result.body?.cancel();
        await abortableDelay(Math.min(30000,Math.max(0, Number.isFinite(wait) ? wait : 0) + Math.random()*250),options.signal); continue;
      }
      if (!result.ok) {
        await result.body?.cancel(); throw new HttpError(result.status, envelope);
      }
      if (result.status === 204) return undefined;
      if (options.binaryResponse) return result;
      const text = await result.text();
      options.signal?.throwIfAborted();
      // A success status with a body that is not JSON (an HTML page, a proxy
      // banner) means the request reached a host that does not serve this
      // operation; report it as an HTTP outcome rather than a parser crash.
      try { return JSON.parse(text) as unknown; } catch { throw new HttpError(result.status, undefined, 'NON_JSON_RESPONSE'); }
    }
  }
}
