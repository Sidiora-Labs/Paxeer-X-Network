export const rankingCategories = ['trending', 'breakout', 'new', 'top_volume', 'unusual', 'movers'] as const;
export type RankingCategory = typeof rankingCategories[number];
const decimalFields = [
  'price', 'priceChange1m', 'priceChange5m', 'priceChange15m', 'priceChange1h', 'priceChange24h',
  'priceChangeDollar1m', 'priceChangeDollar5m', 'priceChangeDollar15m', 'priceChangeDollar1h',
  'priceChangeDollar24h', 'volume24h', 'volume1h', 'volume5m', 'marketCap',
] as const;
export type RankingStats = Partial<Record<typeof decimalFields[number], string>> & { holderCount?: number };
export interface RankingItem { poolAddress: string; score: number; rank: number; stats?: RankingStats | null }
export interface RankingsResponse {
  category: RankingCategory; items: RankingItem[]; total: number; limit: number; offset: number;
}
export interface RankingQuery { limit?: number; offset?: number; signal?: AbortSignal }
export interface HttpClientConfig { baseUrl: string; headers?: HeadersInit }
export class RankingHttpError extends Error {
  readonly status: number;
  readonly body: string;
  constructor(status: number, body: string) {
    super(`Ranking request failed: HTTP ${status}`);
    this.name = 'RankingHttpError'; this.status = status; this.body = body;
  }
}
function object(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}
function nonnegativeInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0;
}
function category(value: unknown): value is RankingCategory {
  return typeof value === 'string' && rankingCategories.some((entry) => entry === value);
}
function stats(value: unknown): value is RankingStats {
  return object(value) && decimalFields.every((key) => value[key] === undefined || typeof value[key] === 'string')
    && (value.holderCount === undefined || nonnegativeInteger(value.holderCount));
}
function item(value: unknown): value is RankingItem {
  return object(value) && typeof value.poolAddress === 'string'
    && typeof value.score === 'number' && Number.isFinite(value.score)
    && nonnegativeInteger(value.rank) && value.rank > 0
    && (value.stats === undefined || value.stats === null || stats(value.stats));
}
function parseRankings(value: unknown, requested: RankingCategory): RankingsResponse {
  if (!object(value) || !category(value.category) || value.category !== requested
    || !Array.isArray(value.items) || !value.items.every(item)
    || !nonnegativeInteger(value.total) || !nonnegativeInteger(value.limit) || !nonnegativeInteger(value.offset)) {
    throw new TypeError('Invalid rankings response');
  }
  return { category: value.category, items: value.items, total: value.total, limit: value.limit, offset: value.offset };
}
export class RankingHttpClient {
  private config: HttpClientConfig | undefined;
  configure(config: HttpClientConfig): void {
    const url = new URL(config.baseUrl);
    if (!['http:', 'https:'].includes(url.protocol) || url.search || url.hash || url.username || url.password) {
      throw new TypeError('Ranking baseUrl must be an HTTP(S) URL without credentials, query or fragment');
    }
    this.config = { baseUrl: url.href.replace(/\/$/, ''), headers: new Headers(config.headers) };
  }
  async getRankings(requested: RankingCategory, query: RankingQuery = {}): Promise<RankingsResponse> {
    if (!this.config) throw new Error('Ranking HTTP client is not configured');
    if (!category(requested)) throw new TypeError('Invalid ranking category');
    const url = new URL(`${this.config.baseUrl}/rankings/${requested}`);
    for (const key of ['limit', 'offset'] as const) {
      const value = query[key];
      if (value !== undefined) {
        if (!nonnegativeInteger(value)) throw new RangeError(`${key} must be a nonnegative safe integer`);
        url.searchParams.set(key, String(value));
      }
    }
    const headers = new Headers(this.config.headers);
    if (!headers.has('Accept')) headers.set('Accept', 'application/json');
    const response = await fetch(url, { headers, signal: query.signal });
    if (!response.ok) throw new RankingHttpError(response.status, await response.text());
    const body: unknown = await response.json();
    return parseRankings(body, requested);
  }
}
export const httpClient = new RankingHttpClient();
export function getRankings(category: RankingCategory, query?: RankingQuery): Promise<RankingsResponse> {
  return httpClient.getRankings(category, query);
}
