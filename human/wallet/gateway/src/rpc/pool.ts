import {
  createPublicClient,
  custom,
  defineChain,
  type Chain,
  type Hex,
  type PublicClient,
} from 'viem';
import { env } from '../env.js';

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
  constructor(code: number, message: string, data: unknown) {
    super(message);
    this.name = 'RpcResponseError';
    this.code = code;
    this.data = data;
  }
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
    const order = this.candidates();
    if (order.length === 0) {
      throw new RpcUnavailableError('no healthy RPC endpoint: every endpoint is down or lagging');
    }
    const failures: string[] = [];
    for (const ep of order) {
      try {
        const result = (await this.post(ep.url, method, params)) as T;
        if (ep.state === 'unknown') ep.state = 'healthy';
        return result;
      } catch (err) {
        if (err instanceof RpcResponseError) throw err;
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

  async getTransactionCount(address: `0x${string}`, blockTag: 'pending' | 'latest' = 'pending'): Promise<number> {
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

  async sendRawTransaction(signed: Hex): Promise<Hex> {
    return this.request<Hex>('eth_sendRawTransaction', [signed]);
  }

  private async post(url: string, method: string, params: unknown[]): Promise<unknown> {
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
    if (body.error) throw new RpcResponseError(body.error.code, body.error.message, body.error.data);
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
