import { poolStatsFields, type PoolStats } from './types.js';
export type { PoolStats } from './types.js';
export interface HttpClientConfig { baseUrl: string; timeoutMs?: number; fetch?: typeof globalThis.fetch }
export interface RequestOptions { signal?: AbortSignal }
export class StatsHttpError extends Error {
  readonly status: number;
  readonly body: string;
  constructor(status: number, body: string) {
    super(`Stats API returned HTTP ${status}`); this.status = status; this.body = body; this.name = 'StatsHttpError';
  }
}
export class StatsResponseError extends Error {
  constructor(message: string) { super(message); this.name = 'StatsResponseError'; }
}
function object(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) throw new StatsResponseError('Expected an object');
  return value as Record<string, unknown>;
}
export function parsePoolStats(value: unknown): PoolStats {
  const row = object(value);
  for (const [key, kind] of Object.entries(poolStatsFields)) {
    const item = row[key];
    if (kind === 'string | null' && item === null) continue;
    const expected = kind === 'string | null' ? 'string' : kind;
    if (typeof item !== expected || (typeof item === 'number' && !Number.isSafeInteger(item))) {
      throw new StatsResponseError(`Invalid pool stats field: ${key}`);
    }
  }
  return row as unknown as PoolStats;
}
function address(value: string): string {
  if (!/^0x[0-9a-fA-F]{40}$/.test(value)) throw new TypeError('Expected a 20-byte EVM address');
  return encodeURIComponent(value);
}
export class StatsHttpClient {
  private config: HttpClientConfig | undefined;
  configure(config: HttpClientConfig): void {
    const url = new URL(config.baseUrl);
    if (!['http:', 'https:'].includes(url.protocol) || url.search || url.hash || url.username || url.password) throw new TypeError('Invalid stats baseUrl');
    const timeoutMs = config.timeoutMs ?? 10_000;
    if (!Number.isFinite(timeoutMs) || timeoutMs < 1 || timeoutMs > 120_000) throw new TypeError('timeoutMs must be between 1 and 120000');
    this.config = { ...config, baseUrl: config.baseUrl.replace(/\/+$/, ''), timeoutMs };
  }
  private async get(path: string, options: RequestOptions): Promise<unknown> {
    const config = this.config;
    if (!config) throw new Error('Stats HTTP client is not configured');
    const controller = new AbortController();
    const abort = () => controller.abort(options.signal?.reason);
    if (options.signal?.aborted) abort();
    options.signal?.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(() => controller.abort(new DOMException('Stats request timed out', 'TimeoutError')), config.timeoutMs);
    try {
      const response = await (config.fetch ?? globalThis.fetch)(config.baseUrl + path, { signal: controller.signal, headers: { Accept: 'application/json' } });
      const text = await response.text();
      if (!response.ok) throw new StatsHttpError(response.status, text);
      try { return JSON.parse(text) as unknown; }
      catch { throw new StatsResponseError('Stats API returned invalid JSON'); }
    } finally {
      clearTimeout(timer); options.signal?.removeEventListener('abort', abort);
    }
  }
  async getPoolStats(poolAddress: string, options: RequestOptions = {}): Promise<PoolStats> {
    return parsePoolStats(await this.get('/stats/' + address(poolAddress), options));
  }
  async getPoolStatsBatch(pools: readonly string[], options: RequestOptions = {}): Promise<Record<string, PoolStats>> {
    if (!pools.length) throw new TypeError('At least one pool is required');
    pools.forEach(address);
    const raw = object(await this.get('/stats/batch?' + new URLSearchParams({ pools: pools.join(',') }), options));
    return Object.fromEntries(Object.entries(raw).map(([key, value]) => [key, parsePoolStats(value)]));
  }
}
export const httpClient = new StatsHttpClient();
