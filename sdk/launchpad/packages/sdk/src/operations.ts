import { operationValidators } from './validators-operations.gen.js';
import { operationDefinitions, type OperationTypes } from './operations.gen.js';
import { SdkClient, decodeV2Contract, CapabilityUnavailableError, type RequestOptions, type V2ContractTypes } from './transport.js';
import { PublicSnapshotRequests, snapshotRequestKey } from './shared-snapshot.js';
export type OperationName = keyof OperationTypes;
export type OperationInput<N extends OperationName> = OperationTypes[N]['input'];
export type OperationResponse<N extends OperationName> = OperationTypes[N]['response'];
function operationValidator(key: string) {
  const validator = Object.hasOwn(operationValidators, key) ? operationValidators[key] : undefined;
  if (!validator) throw new Error(`Missing compiled operation validator ${key}`);
  return validator;
}
/**
 * Client-side timeout floors for generated operations.
 *
 * The contract's `timeoutMs` (operations.gen.ts, 1000 ms for most reads) is the
 * server-side budget measured next to the service. A browser pays more than that
 * before the first byte: DNS and TLS to a cross-region edge, a CORS preflight for
 * every credentialed or custom-header request, and a cold container on first hit.
 * Passing the contract budget straight to fetch aborted healthy 150-500 ms reads
 * as "Request timed out". Each request therefore waits for at least the floor
 * below, or the contract budget when that is larger. An explicit `timeoutMs`
 * passed to `Operations.call` replaces both, and the caller's AbortSignal still
 * cancels at any time.
 */
export const OPERATION_READ_TIMEOUT_FLOOR_MS = 8000;
export const OPERATION_WRITE_TIMEOUT_FLOOR_MS = 10000;
/** The timeout a generated operation is sent with: an explicit override, else max(contract budget, floor). */
export function operationTimeoutMs(definition: { method: string; timeoutMs: number }, override?: number): number {
  if (override !== undefined) return override;
  const floor = definition.method === 'GET' ? OPERATION_READ_TIMEOUT_FLOOR_MS : OPERATION_WRITE_TIMEOUT_FLOOR_MS;
  return Math.max(definition.timeoutMs, floor);
}
type Definition = { wireMajor: 1|2|3; binaryBody: boolean; binaryResponse: boolean; path: string; method: NonNullable<RequestOptions['method']>; service: NonNullable<RequestOptions['service']>; authenticated: boolean; cookieSession: boolean; idempotentRead: boolean; public: boolean; timeoutMs: number; body: string|null; response: string|null; emptyResponse: boolean; idempotencyKey: boolean; parameters: readonly { in: string; name: string; required?: boolean; schema: unknown; explode?: boolean }[] };
export class Operations {
  private readonly publicSnapshots = new PublicSnapshotRequests();
  constructor(readonly client: SdkClient) {}
  async call<N extends OperationName>(name: N, input: OperationInput<N>, options: Pick<RequestOptions, 'signal'|'headers'|'requestId'|'timeoutMs'> = {}): Promise<OperationResponse<N>> {
    const definition: Definition = operationDefinitions[name];
    if (!definition.response && !definition.emptyResponse && !definition.binaryResponse) throw new CapabilityUnavailableError('Operation requires its dedicated streaming transport');
    const supplied = input as { path?: Record<string, unknown>; query?: Record<string, unknown>; body?: unknown };
    let path = definition.path;
    const query = new URLSearchParams();
    for (const location of ['path','query'] as const) {
      const parameters = definition.parameters.filter(p => p.in === location);
      if (!operationValidator(`${name}:${location}`)(supplied[location] ?? {})) throw new TypeError(`Invalid ${name} ${location}`);
      for (const parameter of parameters) {
        const value = supplied[location]?.[parameter.name];
        if (value === undefined || value === null) continue;
        const encoded = Array.isArray(value) ? value.join(',') : typeof value === 'object' ? JSON.stringify(value) : String(value);
        if (location === 'path') path = path.replace(`{${parameter.name}}`, encodeURIComponent(encoded));
        else query.set(parameter.name, encoded);
      }
    }
    const suppliedHeaders = new Headers(options.headers);
    for (const parameter of definition.parameters.filter(p => p.in === 'header' && !['origin','x-csrf-token'].includes(p.name.toLowerCase()))) {
      const value = suppliedHeaders.get(parameter.name);
      if (value === null) { if (parameter.required) throw new TypeError(`Missing ${name} operation header ${parameter.name}`); continue; }
      if (!operationValidator(`${name}:header:${parameter.name}`)(value)) throw new TypeError(`Invalid ${name} operation header ${parameter.name}`);
    }
    if (definition.body) decodeV2Contract(definition.body as keyof V2ContractTypes, supplied.body);
    else if (supplied.body !== undefined && !definition.binaryBody) throw new TypeError('Undocumented operation body');
    if (Object.keys(options.headers ?? {}).some(key => key.toLowerCase() === 'idempotency-key') && !definition.idempotencyKey) throw new TypeError('Operation has no idempotency key contract');
    if (query.size) path += '?' + query.toString();
    const load = async (signal?: AbortSignal): Promise<unknown> => {
      const request: RequestOptions = { method: definition.method, service: definition.service, authenticated: definition.authenticated, cookieSession: definition.cookieSession, idempotentRead: definition.idempotentRead, ...(supplied.body === undefined ? {} : definition.binaryBody ? {rawBody: supplied.body as Blob} : {body: supplied.body}), ...options, timeoutMs: operationTimeoutMs(definition, options.timeoutMs), ...(signal ? {signal} : {}) };
      if (definition.binaryResponse) return this.client.requestGeneratedBinary(path, request, definition.wireMajor);
      if (!definition.response) { await this.client.requestV2Empty(path, request); return undefined; }
      return definition.wireMajor === 3 ? this.client.requestV3(path, definition.response as keyof V2ContractTypes, request) : definition.wireMajor === 1 ? this.client.requestV1Generated(path, definition.response as keyof V2ContractTypes, request) : this.client.requestV2(path, definition.response as keyof V2ContractTypes, request);
    };
    const value = definition.public && definition.method === 'GET' ? await this.publicSnapshots.request(snapshotRequestKey(this.client.config[definition.service === 'media' ? 'mediaOrigin' : definition.service === 'identity' ? 'identityOrigin' : 'apiOrigin'], name, supplied.query ?? {} ) + JSON.stringify(supplied.path ?? {}), load, options.signal) : await load(options.signal);
    options.signal?.throwIfAborted();
    return value as OperationResponse<N>;
  }
  clear(): void { this.publicSnapshots.clear(); }
}
