export interface HttpClientConfig {
  baseUrl: string;
  headers?: Readonly<Record<string, string>>;
  fetch?: typeof globalThis.fetch;
}

export class HttpError extends Error {
  constructor(public readonly status: number, public readonly body: string, public readonly url: string) {
    super(`Indexer data request failed (${status})`);
    this.name = 'HttpError';
  }
}

export interface Trade {
  id: string;
  sender: string;
  isBuy: boolean;
  amountIn: string;
  amountOut: string;
  price: string;
  fee: string;
  blockTimestamp: number;
  txHash: string;
}
export interface RecentTrades { pool: string; trades: Trade[] | null }

export class HttpClient {
  private config?: HttpClientConfig;

  configure(config: HttpClientConfig): void {
    const url = new URL(config.baseUrl);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') throw new TypeError('baseUrl must use HTTP or HTTPS');
    if (url.search || url.hash || url.username || url.password) throw new TypeError('baseUrl must not contain query, fragment or credentials');
    this.config = { ...config, baseUrl: config.baseUrl.replace(/\/+$/, ''), headers: { ...config.headers } };
  }

  async get(path: string, options: { signal?: AbortSignal } = {}): Promise<unknown> {
    if (!this.config) throw new Error('Configure indexer-sdk.httpClient before requesting data');
    if (!path.startsWith('/') || path.startsWith('//') || path.includes('://')) throw new TypeError('Expected a resource path');
    const url = this.config.baseUrl + path;
    const response = await (this.config.fetch ?? globalThis.fetch)(url, {
      method: 'GET', headers: { Accept: 'application/json', ...this.config.headers }, signal: options.signal,
    });
    if (!response.ok) throw new HttpError(response.status, await response.text(), url);
    return response.json() as Promise<unknown>;
  }

  async recentTrades(poolAddress: string, options: { signal?: AbortSignal } = {}): Promise<RecentTrades> {
    if (!/^0x[0-9a-fA-F]{40}$/.test(poolAddress)) throw new TypeError('Invalid pool address');
    const body = await this.get(`/stats/${poolAddress}/transactions`, options);
    if (!isRecord(body) || !Array.isArray(body.transactions) || !body.transactions.every(isTrade)) {
      throw new TypeError('Invalid recent trades response');
    }
    return { pool: poolAddress, trades: body.transactions };
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}
function isTrade(value: unknown): value is Trade {
  if (!isRecord(value)) return false;
  return ['id', 'sender', 'amountIn', 'amountOut', 'price', 'fee', 'txHash'].every(key => typeof value[key] === 'string')
    && typeof value.isBuy === 'boolean' && typeof value.blockTimestamp === 'number' && Number.isSafeInteger(value.blockTimestamp);
}

export const httpClient = new HttpClient();
export const getRecentTrades = (poolAddress: string, options?: { signal?: AbortSignal }): Promise<RecentTrades> => httpClient.recentTrades(poolAddress, options);
