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


export interface WalletBudgetCap {
  readonly id: string; readonly owner: string; readonly asset: string; readonly account: string;
  readonly source_account: string | null; readonly limit: string; readonly configured_limit: string;
  readonly spent: string; readonly remaining: string; readonly carry_cap: string; readonly carried: string;
  readonly period_start: string; readonly period_length: string; readonly expiry: string; readonly revocation_sequence: string;
  readonly closed: boolean; readonly revoked: boolean;
}
export interface WalletGrantCap {
  readonly id: string; readonly owner: string; readonly recipient: string; readonly asset: string;
  readonly per_draw_maximum: string; readonly allowance: string; readonly drawn_total: string; readonly drawn_this_period: string;
  readonly recurring: boolean; readonly window_length: string; readonly window_start: string; readonly expiration: string;
  readonly revocation_sequence: string; readonly revoked_at_sequence: string; readonly revoked: boolean; readonly invoice_settled: boolean;
}
export interface WalletCapsSnapshot {
  readonly state: 'ready' | 'empty'; readonly did: string; readonly account_id: string; readonly network_id: number;
  readonly context: { readonly principal: string; readonly session_id: string; readonly address: string; readonly chain_id: number; readonly expires_at: string };
  readonly observation: { readonly verification: 'state_proven' | 'checkpoint_finalised' | 'settlement_anchored'; readonly state_root: string; readonly sequence: string; readonly observed_head: string; readonly batch: string };
  readonly budgets: readonly WalletBudgetCap[]; readonly grants: readonly WalletGrantCap[];
}
export type WalletCapsState = { readonly state: 'loading' } | { readonly state: 'unavailable' | 'refused'; readonly reason: string } | WalletCapsSnapshot;

export function decodeWalletCaps(value: unknown, address: string, chainId: number): WalletCapsSnapshot {
  const object = (v: unknown): Record<string, unknown> => {
    if (!isRecord(v)) throw new TypeError('invalid caps object');
    return v;
  };
  const text = (v: unknown): string => { if (typeof v !== 'string') throw new TypeError('invalid caps text'); return v; };
  const id = (v: unknown): string => { const s = text(v); if (!/^[0-9a-f]{64}$/.test(s) || /^0{64}$/.test(s)) throw new TypeError('invalid caps identifier'); return s; };
  const uint = (v: unknown, bits: number): string => {
    const s = text(v);
    if (!/^(0|[1-9][0-9]*)$/.test(s) || s.length > 39 || BigInt(s) >= 1n << BigInt(bits)) throw new TypeError('invalid caps quantity');
    return s;
  };
  const flag = (v: unknown): boolean => { if (typeof v !== 'boolean') throw new TypeError('invalid caps flag'); return v; };
  const v = object(value), context = object(v.context), observation = object(v.observation);
  const account_id = id(v.account_id), did = text(v.did);
  if (!/^did:layerx:[0-9a-f]{64}$/.test(did) || did.endsWith('0'.repeat(64))) throw new TypeError('invalid caps DID');
  if (v.state !== 'ready' && v.state !== 'empty') throw new TypeError('incomplete caps response');
  if (typeof v.network_id !== 'number' || !Number.isInteger(v.network_id) || v.network_id < 1 || v.network_id > 0xffffffff) throw new TypeError('invalid caps network');
  if (text(context.address) !== address.toLowerCase() || context.chain_id !== chainId || !Number.isSafeInteger(chainId) || chainId <= 0) throw new TypeError('caps account or chain changed');
  const principal = text(context.principal), expires_at = uint(context.expires_at, 64);
  if (!/^act_[0-9a-f]{64}$/.test(principal) || BigInt(expires_at) <= BigInt(Math.floor(Date.now() / 1000))) throw new TypeError('caps session expired or invalid');
  if (observation.verification !== 'state_proven' && observation.verification !== 'checkpoint_finalised' && observation.verification !== 'settlement_anchored') throw new TypeError('caps verification insufficient');
  const rows = (v: unknown): unknown[] => { if (!Array.isArray(v) || v.length > 65536) throw new TypeError('caps row bound'); return v; };
  const seen = new Set<string>();
  const owner = (r: Record<string, unknown>): string => { if (id(r.owner) !== account_id) throw new TypeError('foreign caps account'); return account_id; };
  const unique = (r: Record<string, unknown>): string => { const key = id(r.id); if (seen.has(key)) throw new TypeError('duplicate caps record'); seen.add(key); return key; };
  const budgets = rows(v.budgets).map((item): WalletBudgetCap => {
    const b = object(item);
    const out = { id: unique(b), owner: owner(b), asset: id(b.asset), account: id(b.account), source_account: b.source_account === null ? null : id(b.source_account),
      limit: uint(b.limit, 128), configured_limit: uint(b.configured_limit, 128), spent: uint(b.spent, 128), remaining: uint(b.remaining, 128),
      carry_cap: uint(b.carry_cap, 128), carried: uint(b.carried, 128), period_start: uint(b.period_start, 64), period_length: uint(b.period_length, 64),
      expiry: uint(b.expiry, 64), revocation_sequence: uint(b.revocation_sequence, 64), closed: flag(b.closed), revoked: flag(b.revoked) };
    if (BigInt(out.spent) > BigInt(out.limit) || BigInt(out.remaining) !== BigInt(out.limit) - BigInt(out.spent) || BigInt(out.period_length) === 0n || BigInt(out.period_start) + BigInt(out.period_length) >= 1n << 64n) throw new TypeError('invalid budget bounds');
    return Object.freeze(out);
  });
  seen.clear();
  const grants = rows(v.grants).map((item): WalletGrantCap => {
    const g = object(item);
    const out = { id: unique(g), owner: owner(g), recipient: id(g.recipient), asset: id(g.asset), per_draw_maximum: uint(g.per_draw_maximum, 128),
      allowance: uint(g.allowance, 128), drawn_total: uint(g.drawn_total, 128), drawn_this_period: uint(g.drawn_this_period, 128),
      recurring: flag(g.recurring), window_length: uint(g.window_length, 64), window_start: uint(g.window_start, 64), expiration: uint(g.expiration, 64),
      revocation_sequence: uint(g.revocation_sequence, 64), revoked_at_sequence: uint(g.revoked_at_sequence, 64), revoked: flag(g.revoked), invoice_settled: flag(g.invoice_settled) };
    if (BigInt(out.per_draw_maximum) === 0n || BigInt(out.allowance) === 0n || BigInt(out.expiration) === 0n || out.recurring !== (BigInt(out.window_length) !== 0n)) throw new TypeError('invalid grant bounds');
    if (BigInt(out.drawn_this_period) > BigInt(out.allowance) || (!out.recurring && BigInt(out.drawn_total) > BigInt(out.allowance))) throw new TypeError('invalid grant bounds');
    return Object.freeze(out);
  });
  if ((v.state === 'empty') !== (budgets.length === 0 && grants.length === 0)) throw new TypeError('unconfirmed empty caps');
  return Object.freeze({ state: v.state, did, account_id, network_id: v.network_id,
    context: Object.freeze({ principal, session_id: id(context.session_id), address: address.toLowerCase(), chain_id: chainId, expires_at }),
    observation: Object.freeze({ verification: observation.verification, state_root: id(observation.state_root), sequence: uint(observation.sequence, 64), observed_head: uint(observation.observed_head, 64), batch: uint(observation.batch, 64) }),
    budgets: Object.freeze(budgets), grants: Object.freeze(grants) });
}

export async function readWalletCaps(provider: { request(args: { method: string; params?: readonly unknown[] }): Promise<unknown> }, address: string): Promise<WalletCapsSnapshot> {
  const chain = await provider.request({ method: 'eth_chainId' });
  if (typeof chain !== 'string' || !/^0x[0-9a-f]+$/i.test(chain)) throw new TypeError('invalid wallet chain');
  const chainId = Number(BigInt(chain));
  const result = await provider.request({ method: 'lx_getWalletCaps', params: [] });
  if (await provider.request({ method: 'eth_chainId' }) !== chain) throw new TypeError('wallet chain changed');
  return decodeWalletCaps(result, address, chainId);
}
