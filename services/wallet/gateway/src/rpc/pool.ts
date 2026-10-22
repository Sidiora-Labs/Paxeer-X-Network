import {
  createPublicClient,
  custom,
  defineChain,
  type Chain,
  keccak256,
  type Hex,
  type PublicClient,
  parseTransaction,
  recoverTransactionAddress,
} from 'viem';
import { env } from '../env.js';
import { BROADCAST_GRACE_MS } from '../chain.js';

export type EndpointState = 'unknown' | 'healthy' | 'lagging' | 'down';

export interface EndpointStatus {
  url: string;
  state: EndpointState;
  head: bigint | null;
  latencyMs: number | null;
  lastError: string | null;
  checkedAt: number | null;
}

export class RpcUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'RpcUnavailableError';
  }
}

export class RpcResponseError extends Error {
  readonly code: number;
  readonly data: unknown;
  /** Index of the configured member that answered with this error, when a pool request names one. */
  memberIndex: number | null = null;
  constructor(code: number, message: string, data: unknown) {
    super(message);
    this.name = 'RpcResponseError';
    this.code = code;
    this.data = data;
  }
}

/**
 * A node refused a read at the `pending` block tag with -32000. Nodes serve no
 * pending block until the sequencer builds one, so this is a definite refusal,
 * never a reason to fail over or retry.
 */
export class PendingReadRefusedError extends RpcResponseError {
  constructor(message: string, data: unknown) {
    super(-32000, message, data);
    this.name = 'PendingReadRefusedError';
  }
}

/**
 * A node answered eth_sendRawTransaction with an error that leaves the
 * transaction's nonce free: the transaction is not in its pool. An answer that
 * the transaction or another one at that nonce is already pooled is not this.
 */
export class TransactionRejectedError extends RpcResponseError {
  constructor(code: number, message: string, data: unknown) {
    super(code, message, data);
    this.name = 'TransactionRejectedError';
  }
}

/**
 * A node refused a broadcast because another transaction holds the sender's nonce: the chain passed
 * it and our transaction is known on no member (nonce too low), or another transaction at that nonce
 * is pooled (replacement underpriced). The caller prepares again, so nothing is ever silently replaced.
 */
export class NonceConflictError extends RpcResponseError {
  constructor(code: number, message: string, data: unknown, readonly member: number | null = null, readonly members = 0) {
    super(code, message, data);
    this.name = 'NonceConflictError';
  }
}

/**
 * A node answered nonce too low, no member knows our hash and the chain's latest count is past our
 * nonce: our own transaction may have mined on a member whose transaction index is off or lagging, so
 * the outcome is unknown. The caller polls eth_getTransactionReceipt by this hash and never prepares again.
 */
export class BroadcastOutcomeUnknownError extends RpcResponseError {
  constructor(code: number, message: string, data: unknown, readonly hash: Hex, readonly nonce: number,
    readonly member: number | null, readonly members: number) {
    super(code, message, data);
    this.name = 'BroadcastOutcomeUnknownError';
  }
}

/** The answer a route gives for a broadcast whose outcome is unknown: poll the receipt by hash, never prepare again. */
export function outcomeUnknownBody(err: BroadcastOutcomeUnknownError): Record<string, unknown> {
  return { error: 'outcome_unknown', tx_hash: err.hash, nonce: err.nonce, retry: 'poll_receipt',
    poll: 'eth_getTransactionReceipt', must_not_resubmit: true, member: err.member, members: err.members };
}

const ALREADY_KNOWN = /already known|already in (?:the )?mempool|already exists/i;
const NONCE_TOO_LOW = /nonce too low/i;
const REPLACEMENT_UNDERPRICED = /replacement (?:transaction )?underpriced/i;

/**
 * Whether any member knows a transaction and, when none does, the index of each member that could not
 * answer and still counts, out of `members` asked (the configured members in order, then the accepting
 * member when it is no longer configured).
 */
export interface TransactionLookup {
  known: boolean;
  unreachable: number[];
  members: number;
}

class RpcTransportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'RpcTransportError';
  }
}

export interface RpcPoolOptions {
  urls: string[];
  chainId: number;
  lagThresholdBlocks: number;
  timeoutMs: number;
  healthIntervalMs: number;
}

export interface PreparedGas {
  gas: bigint;
  maxFeePerGas: bigint;
  maxPriorityFeePerGas: bigint;
}

export interface CallRequest {
  from: `0x${string}`;
  to?: `0x${string}`;
  data?: Hex;
  value?: bigint;
}

let requestId = 0;

export class RpcPool {
  private readonly endpoints: EndpointStatus[];
  private readonly opts: RpcPoolOptions;
  private readonly chain: Chain;
  private timer: NodeJS.Timeout | null = null;
  private client: PublicClient | null = null;
  /** When each member started failing every request, cleared by its next answer. */
  private readonly failingSince = new Map<string, number>();

  constructor(opts: RpcPoolOptions) {
    if (opts.urls.length === 0) throw new Error('RpcPool needs at least one URL');
    this.opts = opts;
    this.endpoints = opts.urls.map((url) => ({
      url,
      state: 'unknown',
      head: null,
      latencyMs: null,
      lastError: null,
      checkedAt: null,
    }));
    this.chain = defineChain({
      id: opts.chainId,
      name: 'Paxeer',
      nativeCurrency: { name: 'Paxeer', symbol: 'PAX', decimals: 18 },
      rpcUrls: { default: { http: opts.urls } },
    });
  }

  status(): EndpointStatus[] {
    return this.endpoints.map((e) => ({ ...e }));
  }

  healthyCount(): number {
    return this.endpoints.filter((e) => e.state === 'healthy').length;
  }

  start(): void {
    if (this.timer) return;
    void this.checkHealth();
    this.timer = setInterval(() => void this.checkHealth(), this.opts.healthIntervalMs);
    this.timer.unref();
  }

  stop(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  async checkHealth(): Promise<EndpointStatus[]> {
    await Promise.all(
      this.endpoints.map(async (ep) => {
        const started = performance.now();
        try {
          const head = BigInt((await this.post(ep.url, 'eth_blockNumber', [])) as string);
          ep.head = head;
          ep.latencyMs = performance.now() - started;
          ep.lastError = null;
          ep.state = 'healthy';
        } catch (err) {
          ep.state = 'down';
          ep.head = null;
          ep.latencyMs = null;
          ep.lastError = err instanceof Error ? err.message : String(err);
        } finally {
          ep.checkedAt = Date.now();
        }
      }),
    );
    const heads = this.endpoints.filter((e) => e.head !== null).map((e) => e.head as bigint);
    if (heads.length > 0) {
      const best = heads.reduce((a, b) => (a > b ? a : b));
      const lag = BigInt(this.opts.lagThresholdBlocks);
      for (const ep of this.endpoints) {
        if (ep.state === 'healthy' && ep.head !== null && best - ep.head > lag) {
          ep.state = 'lagging';
          ep.lastError = `head ${ep.head} trails best head ${best} by more than ${lag} blocks`;
        }
      }
    }
    return this.status();
  }

  private candidates(): EndpointStatus[] {
    const healthy = this.endpoints
      .filter((e) => e.state === 'healthy')
      .sort((a, b) => (a.latencyMs ?? 0) - (b.latencyMs ?? 0));
    const unknown = this.endpoints.filter((e) => e.state === 'unknown');
    return [...healthy, ...unknown];
  }

  async request<T = unknown>(method: string, params: unknown[] = []): Promise<T> {
    return (await this.requestVia<T>(method, params)).result;
  }

  /** request(), also naming the pool member that answered. */
  async requestVia<T = unknown>(method: string, params: unknown[] = []): Promise<{ result: T; member: string }> {
    const order = this.candidates();
    if (order.length === 0) {
      throw new RpcUnavailableError('no healthy RPC endpoint: every endpoint is down or lagging');
    }
    const failures: string[] = [];
    for (const ep of order) {
      try {
        const result = (await this.post(ep.url, method, params)) as T;
        if (ep.state === 'unknown') ep.state = 'healthy';
        return { result, member: ep.url };
      } catch (err) {
        if (err instanceof RpcResponseError) {
          err.memberIndex = this.endpoints.indexOf(ep);
          throw err;
        }
        ep.state = 'down';
        ep.lastError = err instanceof Error ? err.message : String(err);
        failures.push(`${ep.url}: ${ep.lastError}`);
      }
    }
    throw new RpcUnavailableError(`every RPC endpoint failed ${method}: ${failures.join('; ')}`);
  }

  publicClient(): PublicClient {
    if (this.client) return this.client;
    this.client = createPublicClient({
      chain: this.chain,
      transport: custom({
        request: ({ method, params }: { method: string; params?: unknown }) =>
          this.request(method, (params as unknown[] | undefined) ?? []),
      }),
    }) as PublicClient;
    return this.client;
  }

  async getTransactionCount(address: `0x${string}`, blockTag: 'latest' = 'latest'): Promise<number> {
    const hex = await this.request<string>('eth_getTransactionCount', [address, blockTag]);
    return Number(BigInt(hex));
  }

  async simulate(req: CallRequest): Promise<Hex> {
    return this.request<Hex>('eth_call', [callObject(req), 'latest']);
  }

  async prepareGas(
    req: CallRequest & { gas?: bigint; maxFeePerGas?: bigint; maxPriorityFeePerGas?: bigint },
  ): Promise<PreparedGas> {
    const pc = this.publicClient();
    const needFees = req.maxFeePerGas === undefined || req.maxPriorityFeePerGas === undefined;
    const [fees, gas] = await Promise.all([
      needFees ? pc.estimateFeesPerGas() : Promise.resolve(null),
      req.gas !== undefined
        ? Promise.resolve(req.gas)
        : this.request<string>('eth_estimateGas', [callObject(req)]).then((g) => (BigInt(g) * 120n) / 100n),
    ]);
    return {
      gas,
      maxFeePerGas: req.maxFeePerGas ?? fees!.maxFeePerGas,
      maxPriorityFeePerGas: req.maxPriorityFeePerGas ?? fees!.maxPriorityFeePerGas,
    };
  }

  /**
   * Ask every pool member and the node that accepted the broadcast for the transaction by hash. Known
   * anywhere is known. Otherwise not known needs an answer from every member that still counts and at
   * least one answer: a member that has failed every request for longer than the broadcast grace period
   * drops out, so a dead member holds a wallet's nonces for at most that long, and a member that failed
   * more recently is named as unreachable, so a partition is never read as not known.
   */
  async lookupTransaction(hash: Hex, member: string | null): Promise<TransactionLookup> {
    const urls = [...new Set([...this.endpoints.map((ep) => ep.url), ...(member === null ? [] : [member])])];
    const answers = await Promise.allSettled(urls.map((url) => this.post(url, 'eth_getTransactionByHash', [hash])));
    const known = answers.some((a) => a.status === 'fulfilled' && a.value !== null);
    const failed = urls.map((_, i) => i).filter((i) => answers[i]!.status === 'rejected');
    const now = Date.now();
    const unreachable = known ? [] : failed.length === urls.length ? failed
      : failed.filter((i) => now - (this.failingSince.get(urls[i]!) ?? now) <= BROADCAST_GRACE_MS);
    return { known, unreachable, members: urls.length };
  }

  /** lookupTransaction() as a boolean; throws when no member knows it and some member did not answer. */
  async transactionKnown(hash: Hex, member: string | null): Promise<boolean> {
    const { known, unreachable, members } = await this.lookupTransaction(hash, member);
    if (!known && unreachable.length > 0) {
      throw new RpcUnavailableError(`eth_getTransactionByHash for ${hash} went unanswered by members ${unreachable.join(', ')} of ${members}`);
    }
    return known;
  }

  async sendRawTransaction(signed: Hex): Promise<Hex> {
    return (await this.sendRawTransactionVia(signed)).hash;
  }

  /**
   * sendRawTransaction(), also naming the pool member that accepted the transaction; an idempotent
   * answer returns the keccak of the signed bytes as the hash. A member that times out is retried with the same bytes only, so a
   * retry can only answer already known (success with our hash) or accept it. Nonce too low is
   * success when our hash is known on any member; known on none, it is a BroadcastOutcomeUnknownError
   * when the sender's latest count is past our nonce (ours may have mined unindexed) and a
   * NonceConflictError otherwise; replacement underpriced is a NonceConflictError; any other refusal is
   * a TransactionRejectedError.
   */
  async sendRawTransactionVia(signed: Hex): Promise<{ hash: Hex; member: string | null }> {
    const hash = keccak256(signed);
    try {
      const { result, member } = await this.requestVia<Hex>('eth_sendRawTransaction', [signed]);
      return { hash: result, member };
    } catch (err) {
      if (!(err instanceof RpcResponseError)) throw err;
      if (ALREADY_KNOWN.test(err.message)) return { hash, member: null };
      const members = this.endpoints.length;
      if (NONCE_TOO_LOW.test(err.message)) {
        if (await this.transactionKnown(hash, null)) return { hash, member: null };
        const nonce = parseTransaction(signed).nonce ?? 0;
        const from = await recoverTransactionAddress({ serializedTransaction: signed as Parameters<typeof recoverTransactionAddress>[0]['serializedTransaction'] });
        if ((await this.getTransactionCount(from, 'latest')) > nonce) {
          throw new BroadcastOutcomeUnknownError(err.code, err.message, err.data, hash, nonce, err.memberIndex, members);
        }
        throw new NonceConflictError(err.code, err.message, err.data, err.memberIndex, members);
      }
      if (REPLACEMENT_UNDERPRICED.test(err.message)) throw new NonceConflictError(err.code, err.message, err.data, err.memberIndex, members);
      throw new TransactionRejectedError(err.code, err.message, err.data);
    }
  }

  /** One request to one member, recording when the member started failing every request. */
  private async post(url: string, method: string, params: unknown[]): Promise<unknown> {
    try {
      const result = await this.postOnce(url, method, params);
      this.failingSince.delete(url);
      return result;
    } catch (err) {
      if (!this.failingSince.has(url)) this.failingSince.set(url, Date.now());
      throw err;
    }
  }

  private async postOnce(url: string, method: string, params: unknown[]): Promise<unknown> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.opts.timeoutMs);
    let res: Response;
    try {
      res = await fetch(url, {
        method: 'POST',
        headers: { 'content-type': 'application/json' },
        body: JSON.stringify({ jsonrpc: '2.0', id: ++requestId, method, params }),
        signal: controller.signal,
      });
    } catch (err) {
      throw new RpcTransportError(err instanceof Error ? err.message : String(err));
    } finally {
      clearTimeout(timer);
    }
    if (!res.ok) throw new RpcTransportError(`HTTP ${res.status}`);
    let body: { result?: unknown; error?: { code: number; message: string; data?: unknown } };
    try {
      body = (await res.json()) as typeof body;
    } catch {
      throw new RpcTransportError('non-JSON answer');
    }
    if (body.error) {
      if (body.error.code === -32000 && params.includes('pending')) {
        throw new PendingReadRefusedError(body.error.message, body.error.data);
      }
      throw new RpcResponseError(body.error.code, body.error.message, body.error.data);
    }
    if (!('result' in body)) throw new RpcTransportError('answer carries neither result nor error');
    return body.result;
  }
}

function callObject(req: CallRequest): Record<string, string> {
  const o: Record<string, string> = { from: req.from };
  if (req.to) o.to = req.to;
  if (req.data) o.data = req.data;
  if (req.value !== undefined) o.value = `0x${req.value.toString(16)}`;
  return o;
}

export function rpcPoolFromConfig(cfg: {
  RPC_URLS: string[];
  HYPERPAXEER_CHAIN_ID: number;
  RPC_LAG_THRESHOLD_BLOCKS: number;
  RPC_TIMEOUT_MS: number;
  RPC_HEALTH_INTERVAL_MS: number;
}): RpcPool {
  return new RpcPool({
    urls: cfg.RPC_URLS,
    chainId: cfg.HYPERPAXEER_CHAIN_ID,
    lagThresholdBlocks: cfg.RPC_LAG_THRESHOLD_BLOCKS,
    timeoutMs: cfg.RPC_TIMEOUT_MS,
    healthIntervalMs: cfg.RPC_HEALTH_INTERVAL_MS,
  });
}

let shared: RpcPool | null = null;

export function sharedRpcPool(): RpcPool {
  if (shared) return shared;
  shared = rpcPoolFromConfig(env);
  shared.start();
  return shared;
}
