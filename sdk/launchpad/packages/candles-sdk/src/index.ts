export const supportedResolutions = ['1', '5', '15', '60', '240', '1D', '1W', 'D', 'W'] as const;
export type Resolution = (typeof supportedResolutions)[number];
export interface ClientConfiguration { baseUrl: string; timeoutMs?: number }
export interface RequestOptions { signal?: AbortSignal }
export interface HistoryRequest {
  symbol: string;
  resolution: Resolution;
  from?: number;
  to: number;
  countback?: number;
}
// Current Go UDF responses contain display numbers, not lossless accounting amounts.
export interface HistoryBars {
  s: 'ok';
  t: number[];
  o: number[];
  h: number[];
  l: number[];
  c: number[];
  v: number[];
}
export interface NoHistory { s: 'no_data'; nextTime?: number }
export type HistoryResponse = HistoryBars | NoHistory;
export interface UdfConfig {
  supports_search: boolean;
  supports_group_request: boolean;
  supports_marks: boolean;
  supports_timescale_marks: boolean;
  supports_time: boolean;
  exchanges: { value: string; name: string; desc: string }[];
  symbols_types: { name: string; value: string }[];
  supported_resolutions: Resolution[];
  currency_codes: { id: string; code: string; description: string }[];
}
export interface UdfSymbol {
  name: string;
  full_name: string;
  description: string;
  type: string;
  session: string;
  exchange: string;
  listed_exchange: string;
  timezone: string;
  has_intraday: boolean;
  has_weekly_and_monthly: boolean;
  supported_resolutions: Resolution[];
  pricescale: number;
  ticker: string;
  currency_code: string;
  has_empty_bars: boolean;
}

export class UdfResponseError extends Error {
  constructor(message: string) { super(message); this.name = 'UdfResponseError'; }
}
export class CandlesHttpError extends Error {
  readonly status: number;
  constructor(status: number) {
    super(`Candles HTTP request failed: ${status}`);
    this.name = 'CandlesHttpError';
    this.status = status;
  }
}

function record(value: unknown): Record<string, unknown> {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    throw new UdfResponseError('Expected a UDF object');
  }
  return value as Record<string, unknown>;
}
function text(value: unknown, field: string): string {
  if (typeof value !== 'string') throw new UdfResponseError(`Expected string: ${field}`);
  return value;
}
function boolean(value: unknown, field: string): boolean {
  if (typeof value !== 'boolean') throw new UdfResponseError(`Expected boolean: ${field}`);
  return value;
}
function nonnegative(value: unknown, field: string): number {
  if (typeof value !== 'number' || !Number.isFinite(value) || value < 0) {
    throw new UdfResponseError(`Expected finite nonnegative number: ${field}`);
  }
  return value;
}
function timestamp(value: unknown, field: string): number {
  const number = nonnegative(value, field);
  if (!Number.isSafeInteger(number)) throw new UdfResponseError(`Expected safe integer seconds: ${field}`);
  return number;
}
function array<T>(value: unknown, field: string, parse: (item: unknown) => T): T[] {
  if (!Array.isArray(value)) throw new UdfResponseError(`Expected array: ${field}`);
  return value.map(parse);
}
function resolution(value: unknown): Resolution {
  if (typeof value !== 'string' || !supportedResolutions.includes(value as Resolution)) {
    throw new UdfResponseError('Unsupported UDF resolution');
  }
  return value as Resolution;
}

export function parseHistory(value: unknown): HistoryResponse {
  const body = record(value);
  if (body.s === 'no_data') {
    return body.nextTime === undefined ? { s: 'no_data' } : {
      s: 'no_data', nextTime: timestamp(body.nextTime, 'nextTime'),
    };
  }
  if (body.s !== 'ok') throw new UdfResponseError('Unexpected UDF history status');
  const bars: HistoryBars = {
    s: 'ok', t: array(body.t, 't', item => timestamp(item, 't')),
    o: array(body.o, 'o', item => nonnegative(item, 'o')),
    h: array(body.h, 'h', item => nonnegative(item, 'h')),
    l: array(body.l, 'l', item => nonnegative(item, 'l')),
    c: array(body.c, 'c', item => nonnegative(item, 'c')),
    v: array(body.v, 'v', item => nonnegative(item, 'v')),
  };
  const length = bars.t.length;
  if (length > 2000) throw new UdfResponseError('History exceeds 2000 bars');
  if ([bars.o, bars.h, bars.l, bars.c, bars.v].some(values => values.length !== length)) {
    throw new UdfResponseError('UDF history arrays have unequal lengths');
  }
  for (let i = 0; i < length; i++) {
    const time = bars.t[i]!;
    if (i > 0 && time <= bars.t[i - 1]!) throw new UdfResponseError('History timestamps must increase');
    if (bars.h[i]! < Math.max(bars.o[i]!, bars.c[i]!, bars.l[i]!) ||
        bars.l[i]! > Math.min(bars.o[i]!, bars.c[i]!, bars.h[i]!)) {
      throw new UdfResponseError('Invalid OHLC range');
    }
  }
  return bars;
}

export function parseConfig(value: unknown): UdfConfig {
  const body = record(value);
  return {
    supports_search: boolean(body.supports_search, 'supports_search'),
    supports_group_request: boolean(body.supports_group_request, 'supports_group_request'),
    supports_marks: boolean(body.supports_marks, 'supports_marks'),
    supports_timescale_marks: boolean(body.supports_timescale_marks, 'supports_timescale_marks'),
    supports_time: boolean(body.supports_time, 'supports_time'),
    supported_resolutions: array(body.supported_resolutions, 'supported_resolutions', resolution),
    exchanges: array(body.exchanges, 'exchanges', value => {
      const item = record(value);
      return { value: text(item.value, 'value'), name: text(item.name, 'name'), desc: text(item.desc, 'desc') };
    }),
    symbols_types: array(body.symbols_types, 'symbols_types', value => {
      const item = record(value);
      return { name: text(item.name, 'name'), value: text(item.value, 'value') };
    }),
    currency_codes: array(body.currency_codes, 'currency_codes', value => {
      const item = record(value);
      return { id: text(item.id, 'id'), code: text(item.code, 'code'), description: text(item.description, 'description') };
    }),
  };
}

export function parseSymbol(value: unknown): UdfSymbol {
  const body = record(value);
  const pricescale = timestamp(body.pricescale, 'pricescale');
  if (pricescale === 0) throw new UdfResponseError('pricescale must be positive');
  return {
    name: text(body.name, 'name'), full_name: text(body.full_name, 'full_name'),
    description: text(body.description, 'description'), type: text(body.type, 'type'),
    session: text(body.session, 'session'), exchange: text(body.exchange, 'exchange'),
    listed_exchange: text(body.listed_exchange, 'listed_exchange'), timezone: text(body.timezone, 'timezone'),
    has_intraday: boolean(body.has_intraday, 'has_intraday'),
    has_weekly_and_monthly: boolean(body.has_weekly_and_monthly, 'has_weekly_and_monthly'),
    supported_resolutions: array(body.supported_resolutions, 'supported_resolutions', resolution),
    pricescale, ticker: text(body.ticker, 'ticker'), currency_code: text(body.currency_code, 'currency_code'),
    has_empty_bars: boolean(body.has_empty_bars, 'has_empty_bars'),
  };
}

function validateSymbol(symbol: string): void {
  if (typeof symbol !== 'string' || !symbol.trim() || symbol.length > 160) {
    throw new TypeError('symbol must contain 1..160 characters');
  }
}

export class CandlesClient {
  private configuration: Required<ClientConfiguration> | undefined;

  configure(configuration: ClientConfiguration): void {
    const baseUrl = configuration.baseUrl.replace(/\/+$/, '');
    if (!baseUrl || baseUrl.startsWith('//')) throw new TypeError('A UDF base URL is required');
    if (!baseUrl.startsWith('/')) {
      const url = new URL(baseUrl);
      if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
        throw new TypeError('UDF base URL must be HTTP(S), without credentials, query or fragment');
      }
    } else if (baseUrl.includes('?') || baseUrl.includes('#') || baseUrl.includes('\\')) {
      throw new TypeError('Relative UDF base URL must be an origin-relative path');
    }
    const timeoutMs = configuration.timeoutMs ?? 10_000;
    if (!Number.isSafeInteger(timeoutMs) || timeoutMs < 100 || timeoutMs > 60_000) {
      throw new RangeError('timeoutMs must be 100..60000');
    }
    this.configuration = { baseUrl, timeoutMs };
  }

  private async request(path: string, parameters: URLSearchParams, options: RequestOptions): Promise<unknown> {
    const configuration = this.configuration;
    if (!configuration) throw new Error('Configure candles-sdk with the UDF base URL before requesting data');
    const controller = new AbortController();
    const abort = () => controller.abort(options.signal?.reason);
    if (options.signal?.aborted) abort();
    else options.signal?.addEventListener('abort', abort, { once: true });
    const timer = setTimeout(() => controller.abort(new DOMException('Candles request timed out', 'TimeoutError')), configuration.timeoutMs);
    try {
      const query = parameters.toString();
      const response = await fetch(`${configuration.baseUrl}${path}${query ? `?${query}` : ''}`, {
        method: 'GET', signal: controller.signal, headers: { Accept: 'application/json' }, credentials: 'omit',
      });
      if (!response.ok) throw new CandlesHttpError(response.status);
      return await response.json() as unknown;
    } finally {
      clearTimeout(timer);
      options.signal?.removeEventListener('abort', abort);
    }
  }

  async getConfig(options: RequestOptions = {}): Promise<UdfConfig> {
    return parseConfig(await this.request('/config', new URLSearchParams(), options));
  }
  async getSymbol(symbol: string, options: RequestOptions = {}): Promise<UdfSymbol> {
    validateSymbol(symbol);
    return parseSymbol(await this.request('/symbols', new URLSearchParams({ symbol }), options));
  }
  async getHistory(request: HistoryRequest, options: RequestOptions = {}): Promise<HistoryResponse> {
    validateSymbol(request.symbol);
    resolution(request.resolution);
    timestamp(request.to, 'to');
    if (request.from !== undefined) {
      timestamp(request.from, 'from');
      if (request.from > request.to) throw new RangeError('from must not exceed to');
    }
    if (request.countback !== undefined && (!Number.isSafeInteger(request.countback) || request.countback < 1 || request.countback > 2000)) {
      throw new RangeError('countback must be 1..2000');
    }
    if (request.from === undefined && request.countback === undefined) throw new TypeError('History requires from or countback');
    const parameters = new URLSearchParams({ symbol: request.symbol, resolution: request.resolution, to: String(request.to) });
    if (request.from !== undefined) parameters.set('from', String(request.from));
    if (request.countback !== undefined) parameters.set('countback', String(request.countback));
    return parseHistory(await this.request('/history', parameters, options));
  }
  async getTime(options: RequestOptions = {}): Promise<number> {
    return timestamp(await this.request('/time', new URLSearchParams(), options), 'time');
  }
}

export const httpClient = new CandlesClient();
export const getConfig = (options?: RequestOptions): Promise<UdfConfig> => httpClient.getConfig(options);
export const getSymbol = (symbol: string, options?: RequestOptions): Promise<UdfSymbol> => httpClient.getSymbol(symbol, options);
export const getHistory = (request: HistoryRequest, options?: RequestOptions): Promise<HistoryResponse> => httpClient.getHistory(request, options);
export const getTime = (options?: RequestOptions): Promise<number> => httpClient.getTime(options);
