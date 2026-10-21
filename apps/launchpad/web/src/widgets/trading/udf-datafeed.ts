import { dataApiUrl } from '@/core/sdk-config';
import { CHART_UDF_ROUTES, chartsOrigin } from '@/core/charts-udf';
import { unsupportedStatuses } from '@/core/market/unsupported';
import { reportUserFacingError } from '@/core/user-facing-error';
import { decodeV2Contract, type V2ContractTypes } from '@kindle/sdk';
import type { LibrarySymbolInfo, ResolutionString } from '../../../public/charting_library/charting_library';

const POLL_INTERVAL = 10_000;

export type ChartMode = 'price' | 'mcap';
export type ChartStreamState = 'connecting' | 'snapshotting' | 'replaying' | 'live' | 'degraded' | 'offline' | 'resync_required' | 'closed';

type Bar = {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
};

export type CanonicalSupply = { raw: string; decimals: number };
type CanonicalCandle = V2ContractTypes['Candle'];

function displayRatio(numerator: string, denominator: string, supply?: CanonicalSupply): number {
  if (!/^(0|[1-9][0-9]*)$/.test(numerator) || !/^[1-9][0-9]*$/.test(denominator) || numerator.length > 1024 || denominator.length > 1024) throw new Error('Invalid canonical price ratio');
  let n = BigInt(numerator), d = BigInt(denominator);
  if (supply) {
    if (!/^(0|[1-9][0-9]{0,77})$/.test(supply.raw) || BigInt(supply.raw) >= (1n << 256n) || !Number.isSafeInteger(supply.decimals) || supply.decimals < 0 || supply.decimals > 255) throw new Error('Invalid verified token supply');
    n *= BigInt(supply.raw); d *= 10n ** BigInt(supply.decimals);
  }
  if (n === 0n) return 0;
  let exponent = n.toString().length - d.toString().length;
  if (exponent >= 0 ? n < d * 10n ** BigInt(exponent) : n * 10n ** BigInt(-exponent) < d) exponent--;
  const shift = 17 - exponent;
  const scaledN = shift >= 0 ? n * 10n ** BigInt(shift) : n;
  const scaledD = shift >= 0 ? d : d * 10n ** BigInt(-shift);
  let rounded = scaledN / scaledD;
  const remainder = scaledN % scaledD;
  if (remainder * 2n > scaledD || (remainder * 2n === scaledD && rounded % 2n === 1n)) rounded++;
  const value = Number(`${rounded}e${exponent - 17}`);
  if (!Number.isFinite(value) || value === 0) throw new Error('Canonical value cannot be represented by chart display');
  return value;
}

export function canonicalCandleToDisplayBar(value: unknown, mode: ChartMode = 'price', supply?: CanonicalSupply): Bar {
  const candle = decodeV2Contract('Candle', value);
  if (mode === 'mcap' && !supply) throw new Error('Market cap unavailable without verified token supply');
  const time = Date.parse(candle.start);
  if (!Number.isSafeInteger(time) || time < 0) throw new Error('Invalid canonical candle time');
  const convert = (price: CanonicalCandle['open']) => displayRatio(price.numerator, price.denominator, mode === 'mcap' ? supply : undefined);
  const base = candle.open.baseAsset, quote = candle.open.quoteAsset;
  if (base.kind !== 'erc20' || base.chainId !== '125' || base.address === null || base.decimals === null || quote.kind !== 'erc20' || quote.chainId !== '125' || quote.address === null || quote.decimals !== 6) throw new Error('Canonical candle assets unavailable');
  for (const price of [candle.open, candle.high, candle.low, candle.close]) {
    if (price.baseAsset.address?.toLowerCase() !== base.address.toLowerCase() || price.baseAsset.chainId !== base.chainId || price.baseAsset.decimals !== base.decimals || price.quoteAsset.address?.toLowerCase() !== quote.address.toLowerCase() || price.quoteAsset.chainId !== quote.chainId || price.quoteAsset.decimals !== 6 || price.source.kind !== 'gross_execution' || price.source.amountBasis !== 'paid_input' || price.source.position === null || price.source.sourceEventId === null || price.source.sourceReference === null) throw new Error('Canonical candle price provenance mismatch');
  }
  return { time, open: convert(candle.open), high: convert(candle.high), low: convert(candle.low), close: convert(candle.close), volume: displayRatio(candle.volumeUsdlRaw, '1000000') };
}

export async function fetchCanonicalCandlePage(poolAddress: string, resolution: string, from: string, to: string, limit = 50, cursor?: string, signal?: AbortSignal, apiOrigin?: string) {
  const request = decodeV2Contract('CandlesRequest', { poolAddress, interval: chartResolution(resolution).timeframe, from, to, limit, ...(cursor ? { cursor } : {}) });
  const url = new URL(`/v2/pools/${encodeURIComponent(request.poolAddress)}/candles`, apiOrigin ?? dataApiUrl('/'));
  for (const [key, value] of Object.entries({ interval: request.interval, from, to, limit: String(limit), ...(cursor ? { cursor } : {}) })) url.searchParams.set(key, value);
  const response = await fetch(url.toString(), { signal, headers: { 'Accept': 'application/json', 'X-Schema-Version': '2' } });
  if (!response.ok) throw new Error(`Canonical candles: HTTP ${response.status}`);
  if (response.headers.get('X-Schema-Version') !== '2') throw new Error('Canonical candle schema negotiation failed');
  const page = decodeV2Contract('CandlesResponse', await response.json());
  if (page.quality === 'quarantined' || page.watermark?.integrity === 'quarantined') throw new Error('Canonical candle integrity quarantined');
  let previous = -1;
  for (const candle of page.data.items) {
    const start = Date.parse(candle.start);
    if (candle.poolAddress.toLowerCase() !== poolAddress.toLowerCase() || candle.interval !== request.interval || candle.snapshotId !== page.data.snapshotId || candle.seriesModelVersion !== page.data.seriesModelVersion || start <= previous || start < Date.parse(from) || start >= Date.parse(to)) throw new Error('Canonical candle snapshot scope mismatch');
    previous = start;
  }
  return page;
}

function canonicalChartContent(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonicalChartContent).join(',')}]`;
  if (value !== null && typeof value === 'object') return `{${Object.entries(value).sort(([left], [right]) => left < right ? -1 : left > right ? 1 : 0).map(([key, entry]) => `${JSON.stringify(key)}:${canonicalChartContent(entry)}`).join(',')}}`;
  const result = JSON.stringify(value);
  if (result === undefined) throw new Error('Non-JSON canonical candle');
  return result;
}

export class CanonicalCandleRevisionMerge {
  private readonly candles = new Map<string, { revision: bigint; content: string }>();
  accept(value: unknown, pool: string, interval: string): 'apply' | 'ignore' | 'resync' {
    let candle: CanonicalCandle;
    try { candle = decodeV2Contract('Candle', value); } catch { return 'resync'; }
    if (candle.poolAddress.toLowerCase() !== pool.toLowerCase() || candle.interval !== interval) return 'resync';
    const key = `${candle.poolAddress.toLowerCase()}:${candle.interval}:${candle.start}`;
    const revision = BigInt(candle.revision);
    const content = canonicalChartContent(Object.fromEntries(Object.entries(candle).filter(([key]) => key !== 'snapshotId')));
    const old = this.candles.get(key);
    if (old && revision < old.revision) return 'ignore';
    if (old && revision === old.revision) return content === old.content ? 'ignore' : 'resync';
    if (!old && this.candles.size >= 2048) return 'resync';
    this.candles.set(key, { revision, content }); return 'apply';
  }
}

// Same nine-alias vocabulary as the UDF REST boundary; no fallback aliases.
export const CHART_RESOLUTIONS = {
  '1': { timeframe: '1m', seconds: 60 },
  '5': { timeframe: '5m', seconds: 300 },
  '15': { timeframe: '15m', seconds: 900 },
  '60': { timeframe: '1h', seconds: 3600 },
  '240': { timeframe: '4h', seconds: 14400 },
  '1D': { timeframe: '1d', seconds: 86400 },
  'D': { timeframe: '1d', seconds: 86400 },
  '1W': { timeframe: '1w', seconds: 604800 },
  'W': { timeframe: '1w', seconds: 604800 },
} as const;
function chartResolution(resolution: string) {
  if (!Object.prototype.hasOwnProperty.call(CHART_RESOLUTIONS, resolution)) throw new Error(`Unsupported chart resolution: ${resolution}`);
  return CHART_RESOLUTIONS[resolution as keyof typeof CHART_RESOLUTIONS];
}
export function resolutionToWsTimeframe(resolution: string): string { return chartResolution(resolution).timeframe; }
export function resolutionToSeconds(resolution: string): number { return chartResolution(resolution).seconds; }

function barKey(b: Bar): string {
  return `${b.time}|${b.open}|${b.high}|${b.low}|${b.close}|${b.volume}`;
}

export { CHART_UDF_ROUTES, chartsOrigin };

const chartRecord = (value: unknown, what: string): Record<string, unknown> => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error(`Invalid ${what}`);
  return Object.fromEntries(Object.entries(value));
};

/** The supported aliases a charts document lists, without the D/W duplicates of 1D/1W. */
function chartResolutionList(value: unknown): ResolutionString[] {
  if (!Array.isArray(value) || !value.length || !value.every(item => typeof item === 'string' && Object.prototype.hasOwnProperty.call(CHART_RESOLUTIONS, item))) throw new Error('Invalid chart resolutions');
  const aliases = new Set(value.map(item => item === 'D' ? '1D' : item === 'W' ? '1W' : item));
  return Object.keys(CHART_RESOLUTIONS).filter(alias => aliases.has(alias)) as ResolutionString[];
}

/** TradingView library configuration from GET /v2/udf/config. */
export function chartLibraryConfig(value: unknown) {
  const config = chartRecord(value, 'chart configuration');
  if (config.supports_time !== true) throw new Error('Chart configuration without server time');
  return { supported_resolutions: chartResolutionList(config.supported_resolutions), supports_marks: false, supports_timescale_marks: false, supports_time: true, exchanges: [], symbols_types: [] };
}

/** TradingView symbol info from GET /v2/udf/symbols (verified pool metadata). */
export function chartSymbolInfo(value: unknown, poolAddress: string, mode: ChartMode): LibrarySymbolInfo {
  const info = chartRecord(value, 'chart symbol');
  const text = (key: string, max = 200) => { const item = info[key]; if (typeof item !== 'string' || !item || item.length > max) throw new Error(`Invalid chart symbol ${key}`); return item; };
  const pool = poolAddress.toLowerCase();
  if (text('poolAddress') !== pool || text('name') !== pool || text('ticker') !== pool || info.chainId !== '125') throw new Error('Chart symbol does not describe this pool');
  const pricescale = info.pricescale;
  if (typeof pricescale !== 'number' || !Number.isSafeInteger(pricescale) || pricescale < 10 || !/^10*$/.test(String(pricescale))) throw new Error('Invalid chart symbol pricescale');
  if (info.minmov !== 1 || info.currency_code !== 'USDL' || info.timezone !== 'Etc/UTC') throw new Error('Invalid chart symbol quote or timezone');
  const description = text('description', 160);
  text('full_name', 64);
  return {
    name: pool,
    ticker: pool,
    description: mode === 'mcap' ? `${description} (MCap)` : description,
    type: text('type', 32),
    session: text('session', 32),
    exchange: text('exchange', 64),
    listed_exchange: text('listed_exchange', 64),
    timezone: 'Etc/UTC',
    has_intraday: info.has_intraday === true,
    has_weekly_and_monthly: info.has_weekly_and_monthly === true,
    supported_resolutions: chartResolutionList(info.supported_resolutions),
    // Market-cap values are in the thousands: 2 decimal places. Price uses the verified display precision.
    pricescale: mode === 'mcap' ? 100 : pricescale,
    currency_code: 'USDL',
    has_empty_bars: info.has_empty_bars === true,
    minmov: 1,
    format: 'price',
    data_status: 'streaming',
  };
}

/** onReady / resolveSymbol / getServerTime over CHART_UDF_ROUTES; bars and live candles are owned by createReplayUdfDatafeed. */
function createChartMetadataFeed(poolAddress: string, mode: ChartMode, origin?: string) {
  let disposed = false;
  const requests = new Set<AbortController>();
  async function get(path: string, params?: Record<string, string>): Promise<unknown> {
    if (disposed) throw new DOMException('Datafeed disposed', 'AbortError');
    const controller = new AbortController(); requests.add(controller);
    try {
      const url = new URL(path, chartsOrigin(origin));
      if (params) for (const [key, value] of Object.entries(params)) url.searchParams.set(key, value);
      const response = await fetch(url, { signal: controller.signal, headers: { Accept: 'application/json' } });
      if (!response.ok) throw new Error(`Trading charts ${path}: HTTP ${response.status}`);
      const body: unknown = await response.json();
      if (disposed) throw new DOMException('Datafeed disposed', 'AbortError');
      return body;
    } finally { requests.delete(controller); }
  }
  return {
    requestCount: () => requests.size,
    dispose: () => { disposed = true; requests.forEach(controller => controller.abort()); requests.clear(); },
    onReady: (callback: (config: Record<string, unknown>) => void) => {
      get(CHART_UDF_ROUTES.config).then(value => { if (!disposed) callback(chartLibraryConfig(value)); }).catch(() => {});
    },
    searchSymbols: (_input: string, _exchange: string, _type: string, onResult: (result: never[]) => void) => { if (!disposed) onResult([]); },
    resolveSymbol: (_symbolName: string, onResolve: (info: LibrarySymbolInfo) => void, onError: (error: string) => void) => {
      get(CHART_UDF_ROUTES.symbols, { symbol: poolAddress.toLowerCase() })
        .then(value => { if (!disposed) onResolve(chartSymbolInfo(value, poolAddress, mode)); })
        .catch(error => { if (!disposed) onError(reportUserFacingError(error, "Chart can't load right now", 'chart symbol')); });
    },
    getServerTime: (callback: (time: number) => void) => {
      get(CHART_UDF_ROUTES.time).then(value => { if (!disposed && typeof value === 'number' && Number.isSafeInteger(value) && value > 0) callback(value); }).catch(() => {});
    },
  };
}

export function chartMeasurementUtcNs(): string {
  if (typeof performance === 'undefined') throw new Error('Browser monotonic clock unavailable');
  return (BigInt(Math.round(performance.timeOrigin * 1000000)) + BigInt(Math.round(performance.now() * 1000000))).toString();
}
function publishChartMeasurement(value: Record<string, unknown>) {
  if (typeof window === 'undefined') return;
  window.dispatchEvent(new CustomEvent('sidiora:chart-measurement-v1', { detail: value }));
}

export type ChartFreshness = {
  sourceObservedAt: string | null;
  lastReceivedAt: number | null;
  lastSourceHeight: string | null;
  connectionStatus: ChartStreamState;
  lagReason: string | null;
};
export type ChartReplayBinding = { transport: import('@kindle/sdk').StreamTransportV2; signal: AbortSignal };
let chartReplayBinding: ChartReplayBinding | undefined;
const replayBindingListeners = new Set<() => void>();
export function chartReplayListenerCount(): number { return replayBindingListeners.size; }
export function registerChartReplayTransport(binding: ChartReplayBinding): () => void {
  binding.signal.throwIfAborted();
  chartReplayBinding = binding;
  replayBindingListeners.forEach(listener => listener());
  const release = () => {
    if (chartReplayBinding !== binding) return;
    chartReplayBinding = undefined;
    replayBindingListeners.forEach(listener => listener());
  };
  binding.signal.addEventListener('abort', release, { once: true });
  return () => { binding.signal.removeEventListener('abort', release); release(); };
}

type ReplayChartSubscription = {
  guid: string;
  resolution: string;
  tick: (bar: Bar) => void;
  reset: () => void;
  alive: boolean;
  generation: number;
  requests: Set<AbortController>;
  connection?: { send(frame: V2ContractTypes['Subscribe']): void; close(): void };
  operation?: AbortController;
  timer?: ReturnType<typeof setTimeout>;
  retryTimer?: ReturnType<typeof setTimeout>;
  lagTimer?: ReturnType<typeof setTimeout>;
  merge: CanonicalCandleRevisionMerge;
  cursor: string | null;
  sequence: bigint;
  watermark: V2ContractTypes['Watermark'] | null;
  pending: V2ContractTypes['CandleEvent'][];
  pendingBytes: number;
  acknowledged: boolean;
  replaying: boolean;
  loading: number;
  checkpoint: string | null;
  snapshotReady: boolean;
  failures: number;
  retries: number;
  lastTime: number;
  lastBarKey: string;
  freshness: ChartFreshness;
};

export const VALUATION_HISTORY_OPERATION = 'GET /v2/udf/history?mode=total_supply_valuation';
/** The valuation history route answered with a status that means it is not served at this origin (core/trading-charts RegisterProductionUDF only). */
export class ValuationUnavailableError extends Error {
  readonly operation = VALUATION_HISTORY_OPERATION;
  constructor(readonly status: number, readonly origin: string) {
    super(`Historical valuation: HTTP ${status}`);
    this.name = 'ValuationUnavailableError';
  }
}
export type ValuationState = { state: 'ready' } | { state: 'unsupported'; operation: string; status: number; message: string } | { state: 'error'; message: string };

async function historicalValuationBars(candles: CanonicalCandle[], resolution: string, from: number, to: number, signal: AbortSignal, snapshotId: string, valuationOrigin?: string): Promise<Map<string, Bar>> {
  // Total-supply valuation history is served only by core/trading-charts
  // (GET /v2/udf/history, RegisterProductionUDF), reached through the web
  // origin's same-origin rewrite unless an explicit charts origin is given.
  const url = new URL(CHART_UDF_ROUTES.history, chartsOrigin(valuationOrigin));
  for (const [key, value] of Object.entries({ symbol: candles[0]?.poolAddress ?? '', resolution, from: String(from), to: String(to), mode: 'total_supply_valuation' })) url.searchParams.set(key, value);
  const response = await fetch(url, { signal, headers: { Accept: 'application/json', 'X-Schema-Version': '2' } });
  if (!response.ok) throw new ValuationUnavailableError(response.status, url.origin);
  const record = (value: unknown): Record<string, unknown> => { if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('Invalid historical valuation object'); return Object.fromEntries(Object.entries(value)); };
  const data = record(await response.json());
  if (data.s !== 'ok' || data.valuationAvailable !== true || data.valuationBasis !== 'total_supply_valuation' || data.snapshotId !== snapshotId || !Array.isArray(data.t) || !Array.isArray(data.valuations) || data.t.length !== candles.length || data.valuations.length !== candles.length) throw new Error('Historical total supply valuation unavailable or snapshot changed');
  const result = new Map<string, Bar>();
  for (let index = 0; index < candles.length; index++) {
    const candle = candles[index];
    const valuation = record(data.valuations[index]);
    if (data.t[index] !== Date.parse(candle.start) / 1000 || valuation.available !== true || valuation.valuationBasis !== 'total_supply_valuation' || !Array.isArray(valuation.supply) || valuation.supply.length !== 4) throw new Error('Historical valuation identity mismatch');
    const supplyEntries = valuation.supply;
    const prices = [candle.open, candle.high, candle.low, candle.close];
    const values = prices.map((price, priceIndex) => {
      const source = record(supplyEntries[priceIndex]);
      if (typeof source.token !== 'string' || source.token.toLowerCase() !== price.baseAsset.address?.toLowerCase() || typeof source.decimals !== 'number' || source.decimals !== price.baseAsset.decimals || typeof source.totalSupplyRaw !== 'string' || typeof source.supplySourceBlock !== 'string' || source.supplySourceBlock !== price.source.position?.height || typeof source.supplySourceHash !== 'string' || source.supplySourceHash.toLowerCase() !== price.source.position?.blockHash.toLowerCase() || source.alignment !== 'canonical_block_end' || typeof source.valuationRevision !== 'string' || !/^[0-9a-f]{64}$/.test(source.valuationRevision)) throw new Error('Historical supply provenance mismatch');
      return displayRatio(price.numerator, price.denominator, { raw: source.totalSupplyRaw, decimals: source.decimals });
    });
    const bar = canonicalCandleToDisplayBar(candle, 'price');
    result.set(`${candle.start}:${candle.revision}`, { ...bar, open: values[0], high: values[1], low: values[2], close: values[3] });
  }
  return result;
}

export type ChartTableRow = { start: string; interval: string; revision: string; open: string; high: string; low: string; close: string; volumeUsdl: string; volumeTokenRaw: string };
export type ConfirmedChartMarker = { id: string; time: number; candleStart: string; interval: string; side: 'buy' | 'sell'; tokenAmountRaw: string; usdlAmountRaw: string; executionPrice: string; transactionHash: string; actor: string; actorBasis: string };
export type ChartAccessibleView = { rows: ChartTableRow[]; markers: ConfirmedChartMarker[]; annotationState: 'complete' | 'partial' | 'unavailable'; annotationReason: string | null };
export type TradingViewChartMark = { id: string; time: number; color: { border: string; background: string }; text: string; label: string; labelFontColor: string; minSize: number; borderWidth: number; hoveredBorderWidth: number; imageUrl?: string; showLabelWhenImageLoaded?: boolean };
export type ChartMarkDecoration = { name: string; label: string; imageUrl: string | null };
export type ChartMarkPalette = { buy: string; sell: string; label: string };
export type ChartMarkPresentation = { enabled: boolean; palette?: ChartMarkPalette; decorate?: (marker: ConfirmedChartMarker) => ChartMarkDecoration };
const defaultMarkPalette: ChartMarkPalette = { buy: '#8BFFC5', sell: '#FF6367', label: '#020611' };

export function exactChartRatio(numerator: string, denominator: string): string {
  if (!/^(0|[1-9][0-9]*)$/.test(numerator) || !/^[1-9][0-9]*$/.test(denominator) || numerator.length > 1024 || denominator.length > 1024) throw new Error('Invalid exact chart ratio');
  const n = BigInt(numerator), d = BigInt(denominator);
  const whole = n / d; let remainder = n % d;
  if (remainder === 0n) return whole.toString();
  let decimals = '';
  for (let digit = 0; digit < 160 && remainder !== 0n; digit++) { remainder *= 10n; decimals += (remainder / d).toString(); remainder %= d; }
  return remainder === 0n ? `${whole}.${decimals}` : `${numerator}/${denominator}`;
}
export function canonicalCandleTableRow(value: unknown): ChartTableRow {
  const candle = decodeV2Contract('Candle', value);
  canonicalCandleToDisplayBar(candle);
  const price = (value: CanonicalCandle['open']) => exactChartRatio(value.numerator, value.denominator);
  return { start: candle.start, interval: candle.interval, revision: candle.revision, open: price(candle.open), high: price(candle.high), low: price(candle.low), close: price(candle.close), volumeUsdl: exactChartRatio(candle.volumeUsdlRaw, '1000000'), volumeTokenRaw: candle.volumeTokenRaw };
}
export function confirmedChartMarkers(candleValues: unknown[], tradeValues: unknown[]): { markers: ConfirmedChartMarker[]; partial: boolean } {
  const candles = candleValues.map(value => decodeV2Contract('Candle', value));
  const trades = tradeValues.map(value => decodeV2Contract('Trade', value));
  const byId = new Map<string, V2ContractTypes['Trade']>();
  for (const trade of trades) {
    const previous = byId.get(trade.eventId);
    if (previous && canonicalChartContent(previous) !== canonicalChartContent(trade)) throw new Error('Canonical fill identity conflict');
    byId.set(trade.eventId, trade);
  }
  const markers = new Map<string, ConfirmedChartMarker>();
  const joined = new Set<string>();
  let partial = false;
  for (const candle of candles) {
    canonicalCandleToDisplayBar(candle);
    let candleMarkers = 0;
    for (const price of [candle.open, candle.high, candle.low, candle.close]) {
      const source = price.source;
      if (!source.sourceEventId || !source.position || !source.sourceReference) { partial = true; continue; }
      const trade = byId.get(source.sourceEventId);
      if (!trade) { partial = true; continue; }
      if (trade.poolAddress.toLowerCase() !== candle.poolAddress.toLowerCase() || canonicalChartContent(trade.position) !== canonicalChartContent(source.position) || trade.inputDerivationQuality !== 'exact' || trade.paidInputRaw === null) { partial = true; continue; }
      const time = Date.parse(source.observedAt) / 1000;
      if (!Number.isSafeInteger(time) || time < Date.parse(candle.start) / 1000 || time >= Date.parse(candle.start) / 1000 + resolutionToSeconds(Object.keys(CHART_RESOLUTIONS).find(key => resolutionToWsTimeframe(key) === candle.interval)!)) throw new Error('Canonical marker timestamp outside candle');
      const id = `${candle.interval}:${trade.eventId}`;
      if (!markers.has(id)) candleMarkers++;
      const actor = trade.recipientAttribution === 'unavailable' || trade.recipient === null ? trade.sender : trade.recipient;
      const actorBasis = trade.recipientAttribution === 'unavailable' || trade.recipient === null ? 'event_sender' : trade.recipientAttribution;
      if (actorBasis !== 'event_sender' && trade.recipientEvidenceRefs.length === 0) throw new Error('Canonical marker actor evidence unavailable');
      markers.set(id, { id, time, candleStart: candle.start, interval: candle.interval, side: trade.side, tokenAmountRaw: trade.tokenAmountRaw, usdlAmountRaw: trade.usdlAmountRaw, executionPrice: exactChartRatio(price.numerator, price.denominator), transactionHash: trade.position.transactionHash, actor, actorBasis });
      joined.add(trade.eventId);
    }
    if (BigInt(candleMarkers) < BigInt(candle.trades)) partial = true;
  }
  return { markers: [...markers.values()].sort((a, b) => a.time - b.time || a.id.localeCompare(b.id)), partial: partial || joined.size < byId.size };
}
export function tradingViewChartMark(marker: ConfirmedChartMarker, presentation: ChartMarkPresentation = { enabled: true }): TradingViewChartMark {
  const palette = presentation.palette ?? defaultMarkPalette;
  const decoration = presentation.decorate?.(marker);
  const color = marker.side === 'buy' ? palette.buy : palette.sell;
  const label = decoration?.label.slice(0, 2) || (marker.side === 'buy' ? 'B' : 'S');
  const actor = decoration ? `${decoration.name} ${marker.actor}` : marker.actor;
  const mark: TradingViewChartMark = { id: marker.id, time: marker.time, color: { border: color, background: color }, label, labelFontColor: palette.label, minSize: 18, borderWidth: 0, hoveredBorderWidth: 2, text: `${marker.side} · tokens (raw): ${marker.tokenAmountRaw} · USDL (raw): ${marker.usdlAmountRaw} · execution price: ${marker.executionPrice} · transaction: ${marker.transactionHash} · actor: ${actor} (${marker.actorBasis})` };
  if (decoration?.imageUrl) { mark.imageUrl = decoration.imageUrl; mark.showLabelWhenImageLoaded = false; }
  return mark;
}

/**
 * `authority.apiOrigin` is core/api (canonical candles, trades); `authority.valuationOrigin`
 * is core/trading-charts, which serves every CHART_UDF_ROUTES document. Without it both
 * resolve through the page origin's rewrites.
 */
export function createReplayUdfDatafeed(poolAddress: string, mode: ChartMode = 'price', onFreshness: (value: ChartFreshness) => void = () => {}, _supply?: CanonicalSupply, authority?: { apiOrigin: string; valuationOrigin: string }, onView: (view: ChartAccessibleView) => void = () => {}, onValuation: (state: ValuationState) => void = () => {}) {
  const metadata = createChartMetadataFeed(poolAddress, mode, authority?.valuationOrigin);
  let markPresentation: ChartMarkPresentation = { enabled: true };
  const subscriptions = new Map<string, ReplayChartSubscription>();
  const historyRequests = new Set<AbortController>();
  const valuations = new Map<string, Bar>();
  const visibleCandles = new Map<string, CanonicalCandle>();
  const viewMerge = new CanonicalCandleRevisionMerge();
  let annotationGeneration = 0;
  let annotationRequest: AbortController | undefined;
  let markers: ConfirmedChartMarker[] = [];
  let annotationState: ChartAccessibleView['annotationState'] = 'unavailable';
  let annotationReason: string | null = 'Confirmed fill annotations have not loaded';
  const candleIdentity = (candle: CanonicalCandle) => `${candle.interval}:${candle.start}`;
  const publishView = () => {
    if (disposed) return;
    onView({ rows: [...visibleCandles.values()].sort((a, b) => Date.parse(a.start) - Date.parse(b.start)).map(canonicalCandleTableRow), markers: [...markers], annotationState, annotationReason });
  };
  function retain(candle: CanonicalCandle, resolution: string) {
    const decision = viewMerge.accept(candle, poolAddress, resolutionToWsTimeframe(resolution));
    if (decision === 'resync') throw new Error('Visible history revision conflict');
    const key = candleIdentity(candle);
    if (decision === 'apply') {
      if (!visibleCandles.has(key) && visibleCandles.size >= 2048) throw new Error('Visible chart retention exceeded');
      const old = visibleCandles.get(key);
      visibleCandles.set(key, candle);
      publishChartMeasurement({ kind: 'candle_state', received_utc_ns: chartMeasurementUtcNs(), topic: `market.candles.${poolAddress.toLowerCase()}.${candle.interval}`, payload: candle });
      if (old) { markers = markers.filter(marker => marker.candleStart !== candle.start || marker.interval !== candle.interval); annotationState = 'partial'; annotationReason = 'Historical correction requires confirmed marker reconciliation'; }
      publishView();
    }
    return visibleCandles.get(key) ?? candle;
  }
  async function reconcileMarkers() {
    annotationRequest?.abort();
    const controller = new AbortController(); annotationRequest = controller; historyRequests.add(controller);
    const generation = ++annotationGeneration;
    try {
      const trades: V2ContractTypes['Trade'][] = [];
      let cursor: string | undefined;
      let revision: string | null | undefined;
      const seen = new Set<string>();
      for (let pageNumber = 0; pageNumber < 40; pageNumber++) {
        const url = new URL(`/v2/pools/${encodeURIComponent(poolAddress)}/trades`, authority?.apiOrigin ?? dataApiUrl('/'));
        url.searchParams.set('limit', '50'); if (cursor) url.searchParams.set('cursor', cursor);
        const response = await fetch(url, { signal: controller.signal, headers: { Accept: 'application/json', 'X-Schema-Version': '2' } });
        if (!response.ok || response.headers.get('X-Schema-Version') !== '2') throw new Error('Confirmed trade annotations unavailable');
        const page = decodeV2Contract('TradesResponse', await response.json());
        controller.signal.throwIfAborted();
        if (page.quality === 'quarantined' || page.quality === 'stale' || !page.watermark || page.watermark.integrity !== 'healthy' || (revision !== undefined && revision !== page.revision)) throw new Error('Confirmed trade snapshot is unavailable or changed');
        revision = page.revision;
        for (const trade of page.data.items) {
          if (trade.poolAddress.toLowerCase() !== poolAddress.toLowerCase() || BigInt(trade.position.height) > BigInt(page.watermark.height) || (trade.position.height === page.watermark.height && trade.position.blockHash.toLowerCase() !== page.watermark.blockHash.toLowerCase())) throw new Error('Confirmed trade watermark mismatch');
          trades.push(trade);
        }
        if (!page.nextCursor) {
          if (disposed || controller.signal.aborted || generation !== annotationGeneration) return;
          const joined = confirmedChartMarkers([...visibleCandles.values()], trades);
          markers = joined.markers; annotationState = joined.partial || page.quality !== 'complete' ? 'partial' : 'complete';
          annotationReason = annotationState === 'partial' ? 'Only fills with canonical timestamp and execution-price provenance are annotated' : null;
          publishView(); return;
        }
        if (seen.has(page.nextCursor)) throw new Error('Repeated confirmed trade cursor');
        seen.add(page.nextCursor); cursor = page.nextCursor;
      }
      throw new Error('Confirmed annotation window exceeds bounded replay');
    } catch (error) {
      if (!disposed && !controller.signal.aborted && generation === annotationGeneration) { annotationState = 'partial'; annotationReason = error instanceof Error ? error.message : 'Confirmed annotations unavailable'; publishView(); }
    } finally { historyRequests.delete(controller); if (annotationRequest === controller) annotationRequest = undefined; }
  }
  const display = (candle: CanonicalCandle) => { if (mode === 'price') return canonicalCandleToDisplayBar(candle, 'price'); const bar = valuations.get(`${candle.start}:${candle.revision}`); if (!bar) throw new Error('Historical total supply valuation unavailable for candle revision'); return bar; };
  let disposed = false;
  let serial = 0;
  const current = (sub: ReplayChartSubscription) => !disposed && sub.alive && subscriptions.get(sub.guid) === sub;
  const notify = (sub: ReplayChartSubscription, state: ChartStreamState, reason: string | null = null) => {
    if (!current(sub)) return;
    sub.freshness = { ...sub.freshness, connectionStatus: state, lagReason: reason };
    onFreshness({ ...sub.freshness });
    publishChartMeasurement({ kind: 'replay_state', received_utc_ns: chartMeasurementUtcNs(), topic: `market.candles.${poolAddress.toLowerCase()}.${resolutionToWsTimeframe(sub.resolution)}`, generation: sub.generation, sequence: sub.sequence.toString(), replay_complete: sub.snapshotReady && !sub.replaying && sub.acknowledged, pending_bytes: sub.pendingBytes, state, reason });
  };
  function observe(sub: ReplayChartSubscription, watermark: V2ContractTypes['Watermark'] | null) {
    if (watermark === null || watermark.integrity !== 'healthy') throw new Error('Candle source watermark unavailable');
    if (sub.watermark && (BigInt(watermark.height) < BigInt(sub.watermark.height) || (watermark.height === sub.watermark.height && watermark.blockHash.toLowerCase() !== sub.watermark.blockHash.toLowerCase()))) throw new Error('Candle watermark regression');
    sub.watermark = watermark;
    const received = Date.now();
    sub.freshness = { ...sub.freshness, sourceObservedAt: watermark.observedAt, lastReceivedAt: received, lastSourceHeight: watermark.height };
    if (sub.lagTimer !== undefined) clearTimeout(sub.lagTimer);
    const lag = received - Date.parse(watermark.observedAt);
    if (lag > 1000) notify(sub, 'degraded', 'Prices may be a little behind');
    else {
      onFreshness({ ...sub.freshness });
      sub.lagTimer = setTimeout(() => { sub.lagTimer = undefined; notify(sub, 'degraded', 'Prices may be a little behind'); }, Math.max(1, 1001 - lag));
    }
  }
  function applyCandle(sub: ReplayChartSubscription, candle: CanonicalCandle, emit = true) {
    const result = sub.merge.accept(candle, poolAddress, resolutionToWsTimeframe(sub.resolution));
    if (result === 'resync') throw new Error('Candle revision conflict or retention exhausted');
    if (result !== 'apply') return;
    retain(candle, sub.resolution);
    const bar = display(candle);
    if (emit) void reconcileMarkers();
    if (bar.time < sub.lastTime) { sub.reset(); return; }
    sub.lastTime = bar.time;
    const key = barKey(bar);
    if (emit && key !== sub.lastBarKey && current(sub)) { sub.lastBarKey = key; sub.tick(bar); }
  }
  async function readHistory(resolution: string, from: number, to: number, controller: AbortController, limit = 50) {
    const items: CanonicalCandle[] = [];
    let cursor: string | undefined;
    let watermark: V2ContractTypes['Watermark'] | null = null;
    let snapshotId: string | undefined;
    const seen = new Set<string>();
    for (let pageNumber = 0; pageNumber < 100; pageNumber++) {
      const page = await fetchCanonicalCandlePage(poolAddress, resolution, new Date(from * 1000).toISOString(), new Date(to * 1000).toISOString(), Math.min(limit, 50), cursor, controller.signal, authority?.apiOrigin);
      controller.signal.throwIfAborted();
      if (snapshotId !== undefined && snapshotId !== page.data.snapshotId) throw new Error('History snapshot changed during pagination');
      snapshotId = page.data.snapshotId;
      // core/api answers every canonical candle page with quality 'partial'
      // (rest/accounting_v2.go EncodeCachePage); only stale or quarantined
      // pages are refused.
      if (page.quality === 'stale' || page.quality === 'quarantined' || (page.watermark === null && page.data.items.length > 0) || page.watermark?.integrity === 'quarantined') throw new Error('Authoritative history is stale or quarantined');
      watermark = page.watermark;
      for (const candle of page.data.items) {
        if (items.length && Date.parse(candle.start) <= Date.parse(items[items.length - 1].start)) throw new Error('History page overlap or ordering conflict');
        items.push(candle);
        if (items.length > 5000) throw new Error('History exceeds bounded chart window');
      }
      if (!page.nextCursor) {
        if (mode === 'mcap' && items.length) {
          let valued: Map<string, Bar>;
          try { valued = await historicalValuationBars(items, resolution, from, to, controller.signal, page.data.snapshotId, authority?.valuationOrigin); }
          catch (error) {
            if (!disposed && !controller.signal.aborted) {
              if (error instanceof ValuationUnavailableError) onValuation(unsupportedStatuses.has(error.status) ? { state: 'unsupported', operation: error.operation, status: error.status, message: reportUserFacingError(error, 'Market cap history is not available yet', 'chart valuation') } : { state: 'error', message: reportUserFacingError(error, "Market cap chart can't load right now", 'chart valuation') });
              else if (!(error instanceof DOMException && error.name === 'AbortError')) onValuation({ state: 'error', message: reportUserFacingError(error, "Market cap chart can't load right now", 'chart valuation') });
            }
            throw error;
          }
          if (valuations.size + valued.size > 5000) valuations.clear();
          valued.forEach((bar, key) => valuations.set(key, bar));
          if (!disposed) onValuation({ state: 'ready' });
        }
        const reconciled = items.map(candle => retain(candle, resolution));
        void reconcileMarkers();
        return { items: reconciled, watermark };
      }
      if (seen.has(page.nextCursor)) throw new Error('Repeated history cursor');
      seen.add(page.nextCursor); cursor = page.nextCursor;
    }
    throw new Error('History pagination capacity exceeded');
  }
  function drain(sub: ReplayChartSubscription) {
    if (sub.loading) return;
    const pending = sub.pending; sub.pending = []; sub.pendingBytes = 0;
    for (const event of pending) applyCandle(sub, event.payload);
  }
  function finishReplay(sub: ReplayChartSubscription) {
    if (!current(sub) || sub.loading || !sub.checkpoint) return;
    drain(sub); sub.cursor = sub.checkpoint; sub.checkpoint = null;
    sub.replaying = false; sub.retries = 0;
    if (sub.timer !== undefined) clearTimeout(sub.timer); sub.timer = undefined;
    const lag = sub.freshness.sourceObservedAt === null ? Infinity : Date.now() - Date.parse(sub.freshness.sourceObservedAt);
    notify(sub, lag > 1000 ? 'degraded' : 'live', lag > 1000 ? 'Source observation is more than 1000 ms behind' : null);
  }
  function cancelReplay(sub: ReplayChartSubscription) {
    sub.operation?.abort(); sub.operation = undefined;
    sub.connection?.close(); sub.connection = undefined;
    if (sub.retryTimer !== undefined) clearTimeout(sub.retryTimer);
    sub.retryTimer = undefined;
  }
  function close(sub: ReplayChartSubscription) {
    if (!sub.alive) return;
    sub.alive = false; ++sub.generation;
    cancelReplay(sub);
    sub.requests.forEach(request => request.abort()); sub.requests.clear();
    for (const timer of [sub.timer, sub.lagTimer]) if (timer !== undefined) clearTimeout(timer);
    sub.timer = undefined; sub.lagTimer = undefined; sub.pending = []; sub.pendingBytes = 0;
  }
  function schedulePoll(sub: ReplayChartSubscription, immediate = false) {
    if (!current(sub) || sub.timer !== undefined) return;
    const delay = immediate ? 0 : Math.min(60000, POLL_INTERVAL * 2 ** Math.min(sub.failures, 3)) * (0.9 + Math.random() * 0.2);
    sub.timer = setTimeout(() => { sub.timer = undefined; void poll(sub); }, delay);
  }
  async function poll(sub: ReplayChartSubscription) {
    if (!current(sub)) return;
    if (sub.requests.size) { schedulePoll(sub); return; }
    const controller = new AbortController(); sub.requests.add(controller);
    const generation = sub.generation;
    try {
      const now = Math.floor(Date.now() / 1000);
      const page = await readHistory(sub.resolution, Math.max(0, now - resolutionToSeconds(sub.resolution) * 3), now + 60, controller);
      if (!current(sub) || generation !== sub.generation) return;
      for (const candle of page.items) applyCandle(sub, candle);
      sub.failures = 0;
      // An empty page for a pool with no fills carries no watermark; that is an empty series, not an outage.
      if (page.watermark !== null || page.items.length > 0) observe(sub, page.watermark);
    } catch (error) {
      if (current(sub) && !controller.signal.aborted && generation === sub.generation) { sub.failures++; notify(sub, 'offline', reportUserFacingError(error, 'Live updates are paused', 'chart history updates')); }
    } finally { sub.requests.delete(controller); if (current(sub) && sub.freshness.connectionStatus !== 'live') schedulePoll(sub); }
  }
  function fail(sub: ReplayChartSubscription, reason: string) {
    if (!current(sub)) return;
    if (!sub.snapshotReady || sub.pending.length || sub.loading) sub.cursor = null;
    cancelReplay(sub); sub.replaying = true; sub.checkpoint = null; sub.pending = []; sub.pendingBytes = 0;
    notify(sub, 'resync_required', reason); sub.reset(); schedulePoll(sub, true);
    if (sub.retries >= 5) { notify(sub, 'degraded', 'Live updates stopped, try again'); return; }
    const delay = Math.min(30000, 1000 * 2 ** sub.retries++);
    sub.retryTimer = setTimeout(() => { sub.retryTimer = undefined; void recover(sub, false); }, delay);
  }
  async function recover(sub: ReplayChartSubscription, fresh: boolean) {
    if (!current(sub)) return;
    cancelReplay(sub);
    const binding = chartReplayBinding;
    if (!binding || binding.signal.aborted) { notify(sub, 'degraded', 'Live updates are paused'); schedulePoll(sub, true); return; }
    const operation = new AbortController(); sub.operation = operation;
    sub.requests.forEach(request => request.abort());
    const generation = ++sub.generation;
    const valid = () => current(sub) && sub.generation === generation && !operation.signal.aborted;
    const abort = () => { if (valid()) { cancelReplay(sub); notify(sub, 'degraded', 'Live updates are paused'); schedulePoll(sub, true); } };
    binding.signal.addEventListener('abort', abort, { once: true });
    operation.signal.addEventListener('abort', () => binding.signal.removeEventListener('abort', abort), { once: true });
    const topic = `market.candles.${poolAddress.toLowerCase()}.${resolutionToWsTimeframe(sub.resolution)}`;
    const subscriptionId = `chart-${generation}`;
    sub.acknowledged = false; sub.replaying = true; sub.checkpoint = null; sub.pending = []; sub.pendingBytes = 0;
    try {
      if (fresh || sub.cursor === null) {
        sub.snapshotReady = false; notify(sub, 'snapshotting');
        const snapshot = decodeV2Contract('TopicSnapshot', await binding.transport.snapshot([topic], operation.signal));
        if (!valid()) return;
        if (snapshot.topics.length !== 1 || snapshot.topics[0].topic !== topic) throw new Error('Chart snapshot topic mismatch');
        const entry = snapshot.topics[0];
        const candle = decodeV2Contract('Candle', entry.payload);
        if (entry.revision !== candle.revision) throw new Error('Chart snapshot revision mismatch');
        sub.sequence = BigInt(entry.sequence); sub.cursor = snapshot.resumeCursor;
        sub.merge = new CanonicalCandleRevisionMerge(); sub.lastTime = -1;
        const now = Math.floor(Date.now() / 1000);
        const history = await readHistory(sub.resolution, Math.max(0, now - resolutionToSeconds(sub.resolution) * 3), now + 60, operation);
        if (!valid()) return;
        for (const item of history.items) applyCandle(sub, item, false);
        if (history.items.length) { const last = display(history.items[history.items.length - 1]); if (current(sub)) { sub.lastBarKey = barKey(last); sub.tick(last); } }
        applyCandle(sub, candle); observe(sub, entry.watermark); sub.snapshotReady = true;
      }
      notify(sub, 'replaying');
      const ticket = binding.transport.ticket ? decodeV2Contract('StreamTicketIssueData', await binding.transport.ticket([topic], operation.signal)) : undefined;
      if (!valid()) return;
      const connection = await binding.transport.connect({ signal: operation.signal, ticket,
        disconnected: () => { if (valid()) fail(sub, 'Live updates are reconnecting'); },
        receive: value => {
          if (!valid()) return;
          const receivedUtcNs = typeof window === 'undefined' ? null : chartMeasurementUtcNs();
          try {
            if (value !== null && typeof value === 'object' && 'op' in value) {
              const control = decodeV2Contract('StreamControl', value);
              if (control.op === 'heartbeat' && control.subscriptionId === null) return;
              if (control.subscriptionId !== subscriptionId) throw new Error('Unbound chart replay control');
              if (control.op === 'subscribed') { sub.acknowledged = true; return; }
              if (control.op === 'checkpoint') {
                if (!sub.acknowledged || !control.cursor) throw new Error('Replay checkpoint before acknowledgement');
                sub.checkpoint = control.cursor; finishReplay(sub);
                return;
              }
              if (['resnapshot_required', 'integrity_incident', 'subscription_closed', 'error'].includes(control.op)) { sub.cursor = null; throw new Error(control.code ?? control.op); }
              return;
            }
            const frameBytes = new TextEncoder().encode(canonicalChartContent(value)).byteLength;
            if (frameBytes > 256 * 1024) throw new Error('Chart delivery frame exceeds byte bound');
            const event = decodeV2Contract('CandleEvent', value);
            if (event.topic !== topic || event.revision !== event.payload.revision || event.watermark === null || event.watermark.integrity !== 'healthy' || BigInt(event.watermark.height) < BigInt(event.payload.lastPosition.height) || (event.watermark.height === event.payload.lastPosition.height && event.watermark.blockHash.toLowerCase() !== event.payload.lastPosition.blockHash.toLowerCase())) throw new Error('Candle event scope mismatch');
            const next = BigInt(event.sequence);
            if (next <= sub.sequence) { const buffered = sub.pending.find(item => item.sequence === event.sequence); if (buffered && canonicalChartContent(buffered) !== canonicalChartContent(event)) throw new Error('Conflicting buffered replay sequence'); const retained = visibleCandles.get(candleIdentity(event.payload)); if (!buffered && (!retained || BigInt(event.payload.revision) > BigInt(retained.revision))) throw new Error('Unexpected candle revision on old sequence'); if (!buffered && sub.merge.accept(event.payload, poolAddress, resolutionToWsTimeframe(sub.resolution)) === 'resync') throw new Error('Conflicting replayed candle'); return; }
            if (next !== sub.sequence + 1n && (!event.coalesced || event.sourceSequenceFirst === null || event.sourceSequenceLast === null || BigInt(event.sourceSequenceFirst) !== sub.sequence + 1n || BigInt(event.sourceSequenceLast) !== next)) throw new Error('Unresolved candle delivery gap');
            if (sub.pending.length >= 2048 || sub.pendingBytes + frameBytes > 256 * 1024) throw new Error('Chart replay buffer exceeded');
            if (sub.replaying || sub.loading) { sub.pending.push(event); sub.pendingBytes += frameBytes; } else applyCandle(sub, event.payload);
            sub.sequence = next; observe(sub, event.watermark);
            if (receivedUtcNs !== null) publishChartMeasurement({ kind: 'candle_delivery', received_utc_ns: receivedUtcNs, event, topic, sequence: event.sequence, revision: event.revision, source_event_id: event.payload.close.source.sourceEventId, generation });
          } catch (error) { sub.cursor = null; fail(sub, reportUserFacingError(error, 'Live updates are reconnecting', 'chart live update')); }
        },
      });
      if (!valid()) { connection.close(); return; }
      sub.connection = connection;
      connection.send(decodeV2Contract('Subscribe', { op: 'subscribe', subscriptionId, topics: [topic], afterCursor: sub.cursor }));
    } catch (error) { if (valid()) fail(sub, reportUserFacingError(error, 'Live updates are reconnecting', 'chart live updates')); }
  }
  const bindingChanged = () => { historyRequests.forEach(controller => controller.abort()); for (const sub of subscriptions.values()) { sub.cursor = null; sub.retries = 0; void recover(sub, true); } };
  replayBindingListeners.add(bindingChanged);
  const visible = () => { if (document.visibilityState === 'visible') bindingChanged(); };
  const online = () => bindingChanged();
  if (typeof document !== 'undefined') document.addEventListener('visibilitychange', visible);
  if (typeof window !== 'undefined') window.addEventListener('online', online);
  return {
    ...metadata,
    onReady: (callback: (config: Record<string, unknown>) => void) => metadata.onReady(config => callback({ ...config, supports_marks: true })),
    getBars: (_symbol: unknown, resolution: string, period: { from: number; to: number; countBack?: number; firstDataRequest?: boolean }, result: (bars: Bar[], meta: { noData?: boolean }) => void, error: (message: string) => void) => {
      if (disposed) return;
      try { chartResolution(resolution); if (!Number.isSafeInteger(period.from) || !Number.isSafeInteger(period.to) || period.from < 0 || period.to <= period.from || (period.countBack !== undefined && (!Number.isSafeInteger(period.countBack) || period.countBack < 1 || period.countBack > 5000))) throw new Error('Invalid chart history window'); } catch (reason) { error(reportUserFacingError(reason, "Chart can't load right now", 'chart history range')); return; }
      const controller = new AbortController(); historyRequests.add(controller);
      const owners = [...subscriptions.values()].filter(sub => sub.resolution === resolution);
      owners.forEach(sub => { sub.loading++; sub.requests.add(controller); });
      void readHistory(resolution, period.from, period.to, controller).then(page => {
        if (disposed || controller.signal.aborted) return;
        const bars = page.items.map(candle => display(retain(candle, resolution)));
        for (const sub of owners) if (current(sub)) { for (const candle of page.items) applyCandle(sub, candle, false); }
        result(bars, { noData: bars.length === 0 });
      }).catch(reason => { if (!disposed && !controller.signal.aborted) error(reportUserFacingError(reason, "Chart can't load right now", 'chart history')); }).finally(() => {
        historyRequests.delete(controller);
        for (const sub of owners) { sub.requests.delete(controller); sub.loading--; if (current(sub)) { try { if (sub.checkpoint) finishReplay(sub); else if (!sub.replaying) drain(sub); } catch (reason) { sub.cursor = null; fail(sub, reportUserFacingError(reason, 'Live updates are reconnecting', 'chart history merge')); } } }
      });
    },
    subscribeBars: (_symbol: unknown, resolution: string, tick: (bar: Bar) => void, guid: string, reset: () => void = () => {}) => {
      if (disposed) return; chartResolution(resolution);
      const old = subscriptions.get(guid); if (old) close(old);
      const sub: ReplayChartSubscription = { guid, resolution, tick, reset, alive: true, generation: ++serial, requests: new Set(), merge: new CanonicalCandleRevisionMerge(), cursor: null, sequence: 0n, watermark: null, pending: [], pendingBytes: 0, acknowledged: false, replaying: true, loading: 0, checkpoint: null, snapshotReady: false, failures: 0, retries: 0, lastTime: -1, lastBarKey: '', freshness: { sourceObservedAt: null, lastReceivedAt: null, lastSourceHeight: null, connectionStatus: 'connecting', lagReason: null } };
      subscriptions.set(guid, sub); void recover(sub, true);
    },
    unsubscribeBars: (guid: string) => { const sub = subscriptions.get(guid); if (sub) { close(sub); subscriptions.delete(guid); } },
    getMarks: (_symbol: unknown, from: number, to: number, callback: (marks: TradingViewChartMark[]) => void) => {
      if (disposed) return;
      if (!markPresentation.enabled) { callback([]); return; }
      void reconcileMarkers().then(() => { if (!disposed && markPresentation.enabled) callback(markers.filter(marker => marker.time >= from && marker.time <= to).map(marker => tradingViewChartMark(marker, markPresentation))); });
    },
    setMarkPresentation: (presentation: ChartMarkPresentation) => { markPresentation = presentation; },
    markPresentation: () => markPresentation,
    retry: () => bindingChanged(),
    resumeFromCursor: (guid: string, cursor: string) => { const sub = subscriptions.get(guid); if (!sub || !current(sub) || !sub.snapshotReady) throw new Error('Existing authoritative chart snapshot required'); sub.cursor = decodeV2Contract('OpaqueResumeCursor', cursor); sub.retries = 0; void recover(sub, false); },
    replayStatus: () => [...subscriptions.values()].map(sub => ({ guid: sub.guid, generation: sub.generation, replayComplete: sub.snapshotReady && !sub.replaying && sub.acknowledged, pending: sub.pending.length, pendingBytes: sub.pendingBytes, sequence: sub.sequence.toString(), freshness: { ...sub.freshness } })),
    resourceCounts: () => ({
      subscriptions: subscriptions.size,
      requests: metadata.requestCount() + historyRequests.size + [...subscriptions.values()].reduce((count, sub) => count + sub.requests.size, 0),
      sockets: [...subscriptions.values()].filter(sub => sub.connection !== undefined).length,
      timers: [...subscriptions.values()].reduce((count, sub) => count + [sub.timer, sub.retryTimer, sub.lagTimer].filter(timer => timer !== undefined).length, 0),
    }),
    dispose: () => {
      if (disposed) return; disposed = true;
      historyRequests.forEach(controller => controller.abort()); historyRequests.clear();
      for (const sub of subscriptions.values()) close(sub); subscriptions.clear();
      replayBindingListeners.delete(bindingChanged);
      if (typeof document !== 'undefined') document.removeEventListener('visibilitychange', visible);
      if (typeof window !== 'undefined') window.removeEventListener('online', online);
      annotationRequest?.abort(); ++annotationGeneration; visibleCandles.clear(); markers = []; valuations.clear(); metadata.dispose();
    },
  };
}
