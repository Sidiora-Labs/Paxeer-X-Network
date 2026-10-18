import { SdkClient, decodeV2Contract, SnapshotStreamV2, browserStreamTransportV2, type StreamTransportV2, type StreamStateV2, type V2ContractTypes } from '@kindle/sdk';
import type { RankingsResponse, RankingSnapshot } from '../../../../../packages/production-contracts/src/types.gen';
import { orderedTrades, freshness } from './market-presentation';

// The live token-page session rides the v2 stream contract core/api serves:
// GET /v2/snapshots on the api origin and WS /v2/stream on the stream origin.
type Trade = V2ContractTypes['Trade'];
type Watermark = V2ContractTypes['Watermark'];
type Price = V2ContractTypes['Price'];
type PoolStats = V2ContractTypes['PoolStats'];
type TopicSnapshot = V2ContractTypes['TopicSnapshot'];
type StreamEvent = V2ContractTypes['StreamEvent'];
type StreamState = StreamStateV2;
const decodeContract = decodeV2Contract;

/** Origin the page is served from; the public v2 transport only uses it for ticketed streams. */
function applicationOrigin(): string {
  return typeof window === 'undefined' ? 'https://localhost' : window.location.origin;
}

export const marketLimits = Object.freeze({ fills: 1000, rankingPage: 50, holderPage: 100, inactivePools: 20, priceIntervalMs: 500, freshnessIntervalMs: 1000 });
export function marketAddress(value: string): string {
  if (!/^0x[0-9a-fA-F]{40}$/.test(value)) throw new TypeError('Invalid chain125 pool address');
  return value.toLowerCase();
}
export function tradeKey(trade: Trade): string {
  if (trade.position.chainId !== '125') throw new TypeError('Unsupported trade chain');
  if (!/^0x[0-9a-fA-F]{64}$/.test(trade.position.transactionHash) || !/^(0|[1-9][0-9]{0,77})$/.test(trade.position.logIndex)) throw new TypeError('Invalid immutable trade identity');
  return `${trade.position.chainId}:${trade.position.transactionHash.toLowerCase()}:${trade.position.logIndex}`;
}
function canonicalContent(value: unknown): string {
  if (Array.isArray(value)) return '[' + value.map(canonicalContent).join(',') + ']';
  if (value !== null && typeof value === 'object') return '{' + Object.entries(value).sort(([a], [b]) => a.localeCompare(b)).map(([key, item]) => JSON.stringify(key) + ':' + canonicalContent(item)).join(',') + '}';
  const text = JSON.stringify(value); if (text === undefined) throw new TypeError('Non-JSON market content'); return text;
}
export function mergeTradeHistory(previous: readonly Trade[], incoming: readonly Trade[], limit = marketLimits.fills): Trade[] {
  if (!Number.isSafeInteger(limit) || limit < 1 || limit > marketLimits.fills || incoming.length > marketLimits.fills || previous.length > marketLimits.fills) throw new RangeError('Trade history bound exceeded');
  const unique = new Map(previous.map(row => [tradeKey(row), row]));
  for (const row of incoming) unique.set(tradeKey(row), row);
  const rows = [...unique.values()];
  rows.sort((a, b) => {
    for (const field of ['height', 'transactionIndex', 'logIndex'] as const) {
      const x = BigInt(a.position[field]), y = BigInt(b.position[field]);
      if (x !== y) return x > y ? -1 : 1;
    }
    return tradeKey(a).localeCompare(tradeKey(b));
  });
  return orderedTrades(rows.slice(0, limit));
}
export function appendRankingPage(previous: RankingsResponse | null, next: RankingsResponse): RankingsResponse {
  const snapshot = next.data.snapshot;
  if (next.quality === 'quarantined' || snapshot.watermark.integrity === 'quarantined') throw new Error('Ranking integrity quarantined');
  if (snapshot.items.length > marketLimits.rankingPage) throw new RangeError('Ranking page exceeds50');
  if (snapshot.items.some(row => row.category !== snapshot.category || row.modelVersion !== snapshot.modelVersion)) throw new Error('Incompatible ranking page');
  if (new Set(snapshot.items.map(row => marketAddress(row.poolAddress))).size !== snapshot.items.length || new Set(snapshot.items.map(row => row.rank)).size !== snapshot.items.length) throw new Error('Duplicate ranking identity or rank');
  if (!previous) return next;
  const old = previous.data.snapshot;
  if (old.snapshotId !== snapshot.snapshotId || old.category !== snapshot.category || old.modelVersion !== snapshot.modelVersion) throw new Error('Ranking snapshot changed; reload the first page');
  const items = new Map(old.items.map(row => [marketAddress(row.poolAddress), row]));
  for (const row of snapshot.items) items.set(marketAddress(row.poolAddress), row);
  return { ...next, data: { snapshot: { ...snapshot, items: [...items.values()].sort((a, b) => a.rank - b.rank).slice(0, 100) } } };
}
export interface MarketLiveState {
  rows: readonly Trade[];
  price: Price | null;
  priceSequence: string | null;
  priceRevision: string | null;
  stats: PoolStats | null;
  statsGeneratedAt: string | null;
  watermark: Watermark | null;
  generatedAt: string | null;
  heartbeatAt: number | null;
  stream: StreamState;
  error: string | null;
  revision: string | null;
  sequence: string | null;
}
const initialState: MarketLiveState = Object.freeze({ rows: [], price: null, priceSequence: null, priceRevision: null, stats: null, statsGeneratedAt: null, watermark: null, generatedAt: null, heartbeatAt: null, stream: 'idle', error: null, revision: null, sequence: null });
export class MarketSession {
  private state: MarketLiveState = initialState;
  private listeners = new Set<() => void>();
  private revisions = new Map<string, bigint>();
  private readonly stream: SnapshotStreamV2;
  private priceTimer: ReturnType<typeof setTimeout> | null = null;
  private fillTimer: ReturnType<typeof setTimeout> | null = null;
  private retryTimer: ReturnType<typeof setTimeout> | null = null;
  private retries = 0;
  private pendingFills = new Map<string, Trade>();
  private pendingRevisions = new Map<string, bigint>();
  private fillBoundary: StreamEvent | null = null;
  private pendingPrice: StreamEvent | null = null;
  private pendingStats: StreamEvent | null = null;
  private hidden = false;
  private authorizationSignal: AbortSignal | undefined;
  private topicHeads = new Map<string, { revision: bigint; content: string; watermark: Watermark | null }>();
  readonly poolAddress: string;
  constructor(client: SdkClient, poolAddress: string) {
    this.poolAddress = marketAddress(poolAddress);
    const native = browserStreamTransportV2(client, { applicationOrigin: applicationOrigin(), snapshotPath: '/v2/snapshots' }, false);
    const transport: StreamTransportV2 = {
      ...native,
      connect: input => native.connect({ ...input, receive: value => {
        if (value !== null && typeof value === 'object' && 'op' in value) {
          const control = decodeContract('StreamControl', value);
          input.receive(value);
          if ((control.op === 'heartbeat' || control.op === 'checkpoint') && this.stream.state === 'live') this.update({ heartbeatAt: Date.now() });
          return;
        }
        input.receive(value);
      } }),
    };
    this.stream = new SnapshotStreamV2({ transport, maxBuffer: marketLimits.fills,
      installSnapshot: snapshot => this.installSnapshot(snapshot),
      applyEvent: event => this.applyEvent(event),
      onState: (stream, error) => {
        if (stream !== 'live' && stream !== 'replaying') this.clearFrames();
        if (stream === 'live') this.retries = 0;
        this.update({ stream, error: error ?? null });
        if ((stream === 'resync-required' || stream === 'stale') && !this.hidden && !this.retryTimer && this.retries < 3) this.retryTimer = setTimeout(() => { this.retryTimer = null; this.retries++; void this.start(); }, Math.min(5000, 500 * 2 ** this.retries));
      },
    });
  }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getSnapshot = () => this.state;
  getDiagnostics = () => ({ listeners: this.listeners.size, fills: this.state.rows.length, pendingFills: this.pendingFills.size, retainedRevisions: this.revisions.size, topicHeads: this.topicHeads.size, hidden: this.hidden, timers: Number(this.priceTimer !== null) + Number(this.fillTimer !== null) + Number(this.retryTimer !== null) });
  private update(change: Partial<MarketLiveState>) {
    this.state = { ...this.state, ...change };
    if (!this.hidden) for (const listener of this.listeners) { try { listener(); } catch { continue; } }
  }
  private clearFrames() {
    if (this.priceTimer) clearTimeout(this.priceTimer);
    if (this.fillTimer) clearTimeout(this.fillTimer);
    this.priceTimer = null; this.fillTimer = null;
    this.pendingFills.clear(); this.fillBoundary = null;
    this.pendingRevisions.clear();
    this.pendingPrice = null; this.pendingStats = null;
    const retained = new Set(this.state.rows.map(tradeKey));
    for (const identity of this.revisions.keys()) if (!retained.has(identity)) this.revisions.delete(identity);
  }
  private validateHead(event: StreamEvent) {
    const before = this.topicHeads.get(event.topic), revision = BigInt(event.revision), content = canonicalContent(event.payload);
    if (before && (revision < before.revision || (revision === before.revision && content !== before.content))) throw new Error('Market projection revision conflict');
    if (before?.watermark && event.watermark) {
      const old = before.watermark, next = event.watermark;
      if (BigInt(next.height) < BigInt(old.height) || BigInt(next.projectionRevision) < BigInt(old.projectionRevision) || (next.height === old.height && next.blockHash.toLowerCase() !== old.blockHash.toLowerCase())) throw new Error('Market snapshot watermark regression');
    }
    if (event.watermark?.integrity === 'quarantined') throw new Error('Market integrity quarantined');
    return { revision, content, watermark: event.watermark };
  }
  private installSnapshot(snapshot: TopicSnapshot) {
    const events = snapshot.topics.map(entry => decodeContract('StreamEvent', { ...entry, eventId: 'snapshot', occurredAt: snapshot.generatedAt, coalesced: false, sourceSequenceFirst: null, sourceSequenceLast: null }));
    let trade: Trade | null = null, price: Price | null = null, stats: PoolStats | null = null, boundary: StreamEvent | null = null, priceBoundary: StreamEvent | null = null;
    const heads = new Map(this.topicHeads);
    for (const event of events) {
      heads.set(event.topic, this.validateHead(event));
      if (event.topic === `market.trades.${this.poolAddress}`) {
        trade = decodeContract('Trade', event.payload);
        if (marketAddress(trade.poolAddress) !== this.poolAddress) throw new Error('Snapshot trade belongs to another pool');
        tradeKey(trade); boundary = event;
        const current = this.revisions.get(tradeKey(trade));
        if (current !== undefined && BigInt(event.revision) < current) throw new Error('Snapshot trade revision regression');
      } else if (event.topic === `market.price.${this.poolAddress}`) { price = decodeContract('Price', event.payload); priceBoundary = event; }
      else if (event.topic === `market.stats.${this.poolAddress}`) {
        stats = decodeContract('PoolStats', event.payload);
        if (marketAddress(stats.poolAddress) !== this.poolAddress) throw new Error('Snapshot statistics belong to another pool');
      } else throw new Error('Unexpected market snapshot topic');
    }
    if (!trade || !price || !stats || !boundary) throw new Error('Incomplete market snapshot');
    const rows = mergeTradeHistory(this.state.rows, [trade]);
    this.clearFrames(); this.topicHeads = heads;
    this.revisions.set(tradeKey(trade), BigInt(boundary.revision));
    const retained = new Set(rows.map(tradeKey));
    for (const identity of this.revisions.keys()) if (!retained.has(identity)) this.revisions.delete(identity);
    this.update({ rows, price, priceSequence: priceBoundary?.sequence ?? null, priceRevision: priceBoundary?.revision ?? null, stats, statsGeneratedAt: snapshot.generatedAt, generatedAt: snapshot.generatedAt, heartbeatAt: null, watermark: boundary.watermark, revision: boundary.revision, sequence: boundary.sequence });
  }
  private applyEvent(event: StreamEvent) {
    const head = this.validateHead(event);
    if (event.topic === `market.trades.${this.poolAddress}`) {
      const row = decodeContract('Trade', event.payload);
      if (marketAddress(row.poolAddress) !== this.poolAddress) throw new Error('Trade belongs to another pool');
      const key = tradeKey(row), revision = BigInt(event.revision);
      const current = this.pendingRevisions.get(key) ?? this.revisions.get(key);
      if ((current ?? -1n) > revision) return;
      if (current === revision) { const previous = this.pendingFills.get(key) ?? this.state.rows.find(item => tradeKey(item) === key); if (previous && canonicalContent(previous) !== canonicalContent(row)) throw new Error('Equal-revision trade attribution conflict'); }
      if (!this.pendingFills.has(key) && this.pendingFills.size >= marketLimits.fills) throw new Error('Fill render buffer exceeded; durable snapshot/replay required');
      this.pendingRevisions.set(key, revision);
      this.pendingFills.set(key, row);
      this.fillBoundary = event;
      if (!this.hidden && !this.fillTimer) this.fillTimer = setTimeout(() => {
        this.fillTimer = null;
        const rows = mergeTradeHistory(this.state.rows, [...this.pendingFills.values()]);
        for (const [identity, revision] of this.pendingRevisions) this.revisions.set(identity, revision);
        this.pendingRevisions.clear();
        this.pendingFills.clear();
        const retained = new Set(rows.map(tradeKey));
        for (const identity of this.revisions.keys()) if (!retained.has(identity)) this.revisions.delete(identity);
        const boundary = this.fillBoundary; this.fillBoundary = null;
        this.update({ rows, ...(boundary ? { watermark: boundary.watermark, generatedAt: boundary.occurredAt, revision: boundary.revision, sequence: boundary.sequence } : {}) });
      }, marketLimits.priceIntervalMs);
    } else if (event.topic === `market.price.${this.poolAddress}`) {
      decodeContract('Price', event.payload);
      this.pendingPrice = event;
      this.scheduleReplaceableFrame();
    } else if (event.topic === `market.stats.${this.poolAddress}`) {
      const stats = decodeContract('PoolStats', event.payload);
      if (marketAddress(stats.poolAddress) !== this.poolAddress) throw new Error('Statistics belong to another pool');
      this.pendingStats = event;
      this.scheduleReplaceableFrame();
    } else throw new Error('Unexpected market event topic');
    this.topicHeads.set(event.topic, head);
  }
  private scheduleReplaceableFrame() {
    if (!this.hidden && !this.priceTimer) this.priceTimer = setTimeout(() => {
      this.priceTimer = null;
      const price = this.pendingPrice, stats = this.pendingStats;
      this.pendingPrice = null; this.pendingStats = null;
      if (!this.hidden) this.update({ ...(price ? { price: decodeContract('Price', price.payload), priceSequence: price.sequence, priceRevision: price.revision } : {}), ...(stats ? { stats: decodeContract('PoolStats', stats.payload), statsGeneratedAt: stats.occurredAt } : {}) });
    }, marketLimits.priceIntervalMs);
  }
  mergePage(rows: readonly Trade[], revision: string | null) {
    if (revision !== null && !/^[1-9][0-9]{0,77}$/.test(revision)) throw new TypeError('Invalid historical revision');
    const accepted = rows.filter(row => {
      if (marketAddress(row.poolAddress) !== this.poolAddress) throw new Error('Historical trade belongs to another pool');
      const current = this.pendingRevisions.get(tradeKey(row)) ?? this.revisions.get(tradeKey(row));
      if (current !== undefined && revision !== null && BigInt(revision) === current) {
        const previous = this.pendingFills.get(tradeKey(row)) ?? this.state.rows.find(item => tradeKey(item) === tradeKey(row));
        if (previous && canonicalContent(previous) !== canonicalContent(row)) throw new Error('Equal-revision history attribution conflict');
      }
      return current === undefined || (revision !== null && BigInt(revision) >= current);
    });
    const merged = mergeTradeHistory(this.state.rows, accepted);
    if (revision !== null) for (const row of accepted) {
      const key = tradeKey(row);
      this.revisions.set(key, BigInt(revision));
      this.pendingFills.delete(key); this.pendingRevisions.delete(key);
    }
    const retained = new Set([...merged.map(tradeKey), ...this.pendingFills.keys()]);
    for (const identity of this.revisions.keys()) if (!retained.has(identity)) this.revisions.delete(identity);
    this.update({ rows: merged });
  }
  start(signal?: AbortSignal) {
    if (signal) this.authorizationSignal = signal;
    if (this.authorizationSignal?.aborted) return Promise.resolve();
    this.hidden = false;
    if (this.retryTimer) clearTimeout(this.retryTimer);
    this.retryTimer = null;
    this.clearFrames();
    return this.stream.start([`market.trades.${this.poolAddress}`, `market.price.${this.poolAddress}`, `market.stats.${this.poolAddress}`], this.authorizationSignal);
  }
  visibility(hidden: boolean) {
    this.hidden = hidden;
    if (hidden) { this.clearFrames(); if (this.retryTimer) clearTimeout(this.retryTimer); this.retryTimer = null; this.stream.close(); this.update({ stream: 'stale', heartbeatAt: null }); }
    else return this.start();
  }
  status(now: number) { return freshness({ now, generatedAt: this.state.generatedAt, heartbeatAt: this.state.heartbeatAt, contiguous: this.state.stream === 'live', watermark: this.state.watermark }); }
  close() { this.hidden = true; this.stream.close(); this.clearFrames(); if (this.retryTimer) clearTimeout(this.retryTimer); this.retryTimer = null; this.topicHeads.clear(); this.revisions.clear(); this.state = initialState; }
}
export function rankingWindow(snapshot: RankingSnapshot): string {
  return `Model ${snapshot.modelVersion}; computed ${snapshot.computedAt}; lookback unavailable in schema v1`;
}
