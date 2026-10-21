import type { JsonRpcCall, JsonRpcErrorObject, JsonRpcOutcome, JsonRpcParams } from './types.js';

export class JsonRpcError extends Error {
  readonly code: number;
  readonly data: unknown;

  constructor(error: JsonRpcErrorObject) {
    super(error.message);
    this.name = 'JsonRpcError';
    this.code = error.code;
    this.data = error.data;
  }
}

export class JsonRpcTransportError extends Error {
  readonly status: number | null;

  constructor(message: string, status: number | null = null) {
    super(message);
    this.name = 'JsonRpcTransportError';
    this.status = status;
  }
}

export interface JsonRpcClientOptions {
  url: string;
  fetch?: typeof fetch;
  headers?: Record<string, string>;
}

type Envelope = { jsonrpc: '2.0'; id: number; method: string; params: JsonRpcParams };

export class JsonRpcClient {
  readonly url: string;
  private readonly fetchImpl: typeof fetch;
  private readonly headers: Record<string, string>;
  private nextId = 1;

  constructor(options: JsonRpcClientOptions) {
    if (!options.url) throw new Error('JsonRpcClient: url required');
    this.url = options.url;
    this.fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.headers = { ...(options.headers ?? {}) };
  }

  async call<T>(method: string, params: JsonRpcParams = []): Promise<T> {
    const envelope = this.envelope({ method, params });
    const payload = await this.post(envelope);
    const outcome = decodeResponse(payload, envelope.id);
    if (!outcome.ok) throw new JsonRpcError(outcome.error);
    return outcome.result as T;
  }

  async batch(calls: readonly JsonRpcCall[]): Promise<JsonRpcOutcome[]> {
    if (calls.length === 0) return [];
    const envelopes = calls.map((call) => this.envelope(call));
    const payload = await this.post(envelopes);
    if (!Array.isArray(payload)) {
      if (isRecord(payload) && isRecord(payload.error)) {
        throw new JsonRpcError(decodeError(payload.error));
      }
      throw new JsonRpcTransportError('batch response is not an array');
    }
    const byId = new Map<number, unknown>();
    for (const entry of payload) {
      if (!isRecord(entry) || typeof entry.id !== 'number') {
        throw new JsonRpcTransportError('batch response entry carries no numeric id');
      }
      if (byId.has(entry.id)) throw new JsonRpcTransportError('batch response repeats an id');
      byId.set(entry.id, entry);
    }
    return envelopes.map((envelope) => {
      const entry = byId.get(envelope.id);
      if (entry === undefined) {
        return { ok: false, error: { code: -32603, message: `no response for ${envelope.method}` } };
      }
      return decodeResponse(entry, envelope.id);
    });
  }

  private envelope(call: JsonRpcCall): Envelope {
    const id = this.nextId;
    this.nextId += 1;
    return { jsonrpc: '2.0', id, method: call.method, params: call.params };
  }

  private async post(body: Envelope | Envelope[]): Promise<unknown> {
    let response: Response;
    try {
      response = await this.fetchImpl(this.url, {
        method: 'POST',
        headers: { 'content-type': 'application/json', accept: 'application/json', ...this.headers },
        body: JSON.stringify(body),
      });
    } catch (err) {
      throw new JsonRpcTransportError(err instanceof Error ? err.message : 'request failed');
    }
    const text = await response.text();
    let payload: unknown;
    try {
      payload = JSON.parse(text);
    } catch {
      throw new JsonRpcTransportError(`response is not JSON (HTTP ${response.status})`, response.status);
    }
    if (!response.ok && !(isRecord(payload) || Array.isArray(payload))) {
      throw new JsonRpcTransportError(`HTTP ${response.status}`, response.status);
    }
    return payload;
  }
}

export function decodeResponse(value: unknown, id: number): JsonRpcOutcome {
  if (!isRecord(value) || value.jsonrpc !== '2.0' || value.id !== id) {
    throw new JsonRpcTransportError('response does not answer the request id');
  }
  const hasResult = 'result' in value;
  const hasError = 'error' in value;
  if (hasResult === hasError) throw new JsonRpcTransportError('response carries neither or both of result and error');
  if (hasError) return { ok: false, error: decodeError(value.error) };
  return { ok: true, result: value.result };
}

function decodeError(value: unknown): JsonRpcErrorObject {
  if (!isRecord(value) || typeof value.code !== 'number' || !Number.isInteger(value.code) || typeof value.message !== 'string') {
    throw new JsonRpcTransportError('malformed JSON-RPC error');
  }
  return 'data' in value ? { code: value.code, message: value.message, data: value.data } : { code: value.code, message: value.message };
}

export function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
