import { EndpointClient } from './endpoint.js';
import { JsonRpcError, isRecord } from './rpc.js';
import type {
  KernelAvailabilityState,
  KernelBackendName,
  KernelDocument,
  KernelRead,
  KernelStatus,
  KernelUnavailableState,
} from './types.js';

export const KERNEL_UNAVAILABLE_CODE = -32010;
export const DEFAULT_KERNEL_AVAILABILITY_TTL_MS = 5_000;

const KERNEL_BACKENDS: readonly KernelBackendName[] = [
  'core_agent_boundary',
  'public_core',
  'independent_receipt_authority',
  'identity',
  'program_registry',
];

export interface KernelAvailabilityOptions {
  ttlMs?: number;
  now?: () => number;
}

export function availabilityFromStatus(status: KernelStatus): KernelAvailabilityState {
  if (status.reason === 'available') return { available: true, reason: 'available' };
  return { available: false, reason: status.reason, backend: null };
}

export function kernelUnavailableFrom(error: unknown): KernelUnavailableState | null {
  if (!(error instanceof JsonRpcError) || error.code !== KERNEL_UNAVAILABLE_CODE) return null;
  const data = error.data;
  if (!isRecord(data) || data.code !== 'kernel_unavailable') return null;
  if (data.reason !== 'not_configured' && data.reason !== 'unreachable') return null;
  const backend =
    typeof data.backend === 'string' && (KERNEL_BACKENDS as readonly string[]).includes(data.backend)
      ? (data.backend as KernelBackendName)
      : null;
  return { available: false, reason: data.reason, backend };
}

export class KernelAvailability {
  private readonly endpoint: EndpointClient;
  private readonly ttlMs: number;
  private readonly now: () => number;
  private cached: { state: KernelAvailabilityState; at: number } | null = null;
  private inflight: Promise<KernelAvailabilityState> | null = null;

  constructor(endpoint: EndpointClient, options: KernelAvailabilityOptions = {}) {
    const ttl = options.ttlMs ?? DEFAULT_KERNEL_AVAILABILITY_TTL_MS;
    if (!Number.isInteger(ttl) || ttl < 0) throw new RangeError('ttlMs must be a non-negative integer');
    this.endpoint = endpoint;
    this.ttlMs = ttl;
    this.now = options.now ?? Date.now;
  }

  async current(): Promise<KernelAvailabilityState> {
    if (this.cached !== null && this.now() - this.cached.at < this.ttlMs) return this.cached.state;
    if (this.inflight !== null) return this.inflight;
    this.inflight = this.endpoint
      .getNetwork()
      .then((network) => {
        const state = availabilityFromStatus(network.kernel);
        this.cached = { state, at: this.now() };
        return state;
      })
      .finally(() => {
        this.inflight = null;
      });
    return this.inflight;
  }

  record(state: KernelAvailabilityState): void {
    this.cached = { state, at: this.now() };
  }

  invalidate(): void {
    this.cached = null;
  }
}

export class KernelClient {
  readonly endpoint: EndpointClient;
  readonly availability: KernelAvailability;

  constructor(endpoint: EndpointClient, availability: KernelAvailability = new KernelAvailability(endpoint)) {
    this.endpoint = endpoint;
    this.availability = availability;
  }

  getAccount(accountId: string): Promise<KernelRead<KernelDocument>> {
    return this.read('lx_getAccount', [hex32(accountId, 'account id')]);
  }

  getBalances(did: string): Promise<KernelRead<KernelDocument>> {
    if (!/^[A-Za-z0-9._:-]{1,255}$/.test(did)) throw new RangeError('did is malformed');
    return this.read('lx_getBalances', [did]);
  }

  getSequence(accountId: string): Promise<KernelRead<KernelDocument>> {
    return this.read('lx_getSequence', [hex32(accountId, 'account id')]);
  }

  getReceipt(activityId: string): Promise<KernelRead<KernelDocument>> {
    return this.read('lx_getReceipt', [hex32(activityId, 'activity id')]);
  }

  getActivityStatus(activityId: string): Promise<KernelRead<KernelDocument>> {
    return this.read('lx_getActivityStatus', [hex32(activityId, 'activity id')]);
  }

  private async read(method: string, params: readonly unknown[]): Promise<KernelRead<KernelDocument>> {
    const state = await this.availability.current();
    if (!state.available) return state;
    let result: unknown;
    try {
      result = await this.endpoint.rpc.call(method, params);
    } catch (error) {
      const unavailable = kernelUnavailableFrom(error);
      if (unavailable === null) throw error;
      this.availability.record(unavailable);
      return unavailable;
    }
    if (!isRecord(result)) throw new TypeError(`${method} answered a non-object result`);
    return { available: true, result };
  }
}

function hex32(value: string, what: string): string {
  if (!/^[0-9a-fA-F]{64}$/.test(value) || /^0{64}$/.test(value)) throw new RangeError(`${what} must be 32 bytes of hex`);
  return value;
}
