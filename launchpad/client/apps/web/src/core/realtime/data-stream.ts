/**
 * Public market data stream: core/api `GET /v2/snapshots` + WS `/v2/stream`
 * (api-sockets role). One shared, reference-counted connection per SdkClient
 * multiplexes every consumer's topics (CanonicalDataStream below):
 *
 *   market.trades.<pool>   Trade           (MarketTradesEvent)
 *   market.stats.<pool>    PoolStats       (StatsEvent)
 *   market.price.<pool>    Price           (MarketPriceEvent)
 *   market.rankings.<cat>  RankingSnapshot (RankingEvent, replace: true)
 *
 * On every (re)connect the union of topics is snapshotted, installed into each
 * member, then the socket resumes from the snapshot's cursor; the SDK transport
 * pings every 4 s so heartbeats prove liveness on idle topics. A connection
 * carries at most 20 topics (core/api broker.CanonicalTopics); exceeding the
 * union raises TopicConnectionPolicyError rather than silently dropping one.
 * The legacy hub (`/ws`, `{type:"subscribe",channels,pools}`) is dev-only in
 * production and is not used by any page.
 */
import type { SdkClient as MarketClient } from '@kindle/sdk';

/** Ranking categories core/api accepts on public market.rankings topics (shared/auth ValidateStreamBinding). */
export const RANKING_CATEGORIES = ['trending', 'breakout', 'volume', 'gainers', 'losers', 'movers', 'unusual', 'new'] as const;
export type MarketRankingCategory = (typeof RANKING_CATEGORIES)[number];
export function isRankingCategory(value: string): value is MarketRankingCategory {
  return (RANKING_CATEGORIES as readonly string[]).includes(value);
}
/** Per-pool public topic families. */
export type MarketPoolFamily = 'trades' | 'stats' | 'price';

function poolKey(pool: string): string {
  if (!/^0x[0-9a-fA-F]{40}$/.test(pool)) throw new TypeError('Invalid pool address');
  return pool.toLowerCase();
}

/** Canonical v2 public market topic names. */
export const marketTopics = Object.freeze({
  pool: (family: MarketPoolFamily, pool: string) => `market.${family}.${poolKey(pool)}`,
  trades: (pool: string) => `market.trades.${poolKey(pool)}`,
  stats: (pool: string) => `market.stats.${poolKey(pool)}`,
  price: (pool: string) => `market.price.${poolKey(pool)}`,
  rankings: (category: MarketRankingCategory) => {
    if (!isRankingCategory(category)) throw new TypeError('Unknown ranking category');
    return `market.rankings.${category}`;
  },
});

/** Public snapshot route paired with WS /v2/stream. */
export const PUBLIC_SNAPSHOT_PATH = '/v2/snapshots';

export interface MarketTopicSubscription {
  topics: string[];
  /** Topic snapshot installed on every (re)connect, filtered to this subscription's topics. Must commit synchronously. */
  onSnapshot?(snapshot: V2TopicSnapshot): void;
  onEvent(event: V2StreamEvent): void;
  onStatus?(status: ReconnectStatus): void;
  onError?(error: unknown): void;
  signal?: AbortSignal;
}

/**
 * Join the shared public stream for `client`. Returns a release function; the
 * connection closes when its last subscriber releases. No-op without a
 * WebSocket or an application origin (SSR).
 */
export function subscribeMarketTopics(client: MarketClient, subscription: MarketTopicSubscription, applicationOrigin?: string): () => void {
  const origin = applicationOrigin ?? (typeof window !== 'undefined' && window.location ? window.location.origin : null);
  if (!origin || typeof WebSocket === 'undefined' || subscription.signal?.aborted || subscription.topics.length === 0) return () => {};
  const { stream, release } = acquireCanonicalDataStream(client, { applicationOrigin: origin, snapshotPath: PUBLIC_SNAPSHOT_PATH }, false, '');
  let detach: () => void;
  try {
    detach = stream.subscribe({
      topics: subscription.topics,
      installSnapshot: snapshot => { subscription.onSnapshot?.(snapshot); },
      onEvent: event => subscription.onEvent(event),
      onStatus: status => subscription.onStatus?.(status),
      onError: error => subscription.onError?.(error),
      signal: subscription.signal,
    });
  } catch (error) { release(); throw error; }
  let released = false;
  return () => { if (released) return; released = true; detach(); release(); };
}

import { RafScheduler, CoalescingBuffer } from './raf-batch';
import { SnapshotStreamV2, type V2StreamEvent, type V2TopicSnapshot } from '@kindle/sdk';
import { ReconnectingStream, type ReconnectStatus } from '@kindle/sdk';
import { browserStreamTransportV2, type BrowserStreamV2Scope } from '@kindle/sdk';
import { SdkClient, decodeV2Contract } from '@kindle/sdk';
export class TopicConnectionPolicyError extends Error { readonly code = 'TOPIC_CONNECTION_POLICY_REQUIRED'; }
export interface CanonicalSubscription {
  topics: string[];
  installSnapshot(snapshot: V2TopicSnapshot): void;
  onEvent(event: V2StreamEvent): void;
  onStatus?(status: ReconnectStatus): void;
  onError?(error: unknown): void;
  signal?: AbortSignal;
}
export class CanonicalDataStream {
  private readonly members = new Map<symbol, CanonicalSubscription>();
  private readonly detachMembers = new Map<symbol, () => void>();
  private generation = 0;
  private disposed = false;
  private readonly unready = new Set<CanonicalSubscription>();
  private readonly scheduler = new RafScheduler();
  private readonly replaceable = new CoalescingBuffer<V2StreamEvent>({ maxSize: 5000 });
  private readonly stream: SnapshotStreamV2;
  private readonly reconnect: ReconnectingStream;
  private union: string[] = [];
  private authorization = '';
  private readonly snapshots = new Map<string, V2TopicSnapshot['topics'][number]>();
  private lastSnapshot: V2TopicSnapshot | undefined;
  constructor(client: SdkClient, scope: BrowserStreamV2Scope, readonly authenticated: boolean, authorizationScope: string) {
    if (authenticated && !authorizationScope) throw new TypeError('Subject and permission scope required');
    this.authorization = authorizationScope;
    const report = (member: CanonicalSubscription, error: unknown) => { try { member.onError?.(error); } catch {} };
    this.stream = new SnapshotStreamV2({
      transport: browserStreamTransportV2(client, scope, authenticated),
      installSnapshot: snapshot => {
        const generation = this.generation;
        this.scheduler.cancel(); this.replaceable.clear();
        this.snapshots.clear(); for (const topic of snapshot.topics) this.snapshots.set(topic.topic, topic); this.lastSnapshot = snapshot;
        for (const member of [...this.members.values()]) { if (generation !== this.generation) return; if (![...this.members.values()].includes(member)) continue; try { this.installMemberSnapshot(member, { ...snapshot, topics: snapshot.topics.filter(topic => member.topics.includes(topic.topic)) }); if (generation !== this.generation) return; this.unready.delete(member); } catch (error) { if (generation !== this.generation || ![...this.members.values()].includes(member)) return; this.unready.add(member); report(member, error); } }
      },
      applyEvent: event => {
        this.snapshots.set(event.topic, { topic: event.topic, payload: event.payload, sequence: event.sequence, revision: event.revision, watermark: event.watermark });
      },
      onEvent: event => {
        const generation = this.generation;
        const fanout = (value: V2StreamEvent) => { if (generation !== this.generation) return; for (const member of [...this.members.values()]) if (generation === this.generation && [...this.members.values()].includes(member) && !this.unready.has(member) && member.topics.includes(value.topic)) { try { member.onEvent(structuredClone(value)); } catch (error) { report(member, error); } } };
        if (/^(?:market\.(?:candles|price|stats)\.|private\.portfolio\.)/.test(event.topic)) {
          const key = event.topic.startsWith('market.candles.') ? `${event.topic}:${decodeV2Contract('Candle', event.payload).start}` : event.topic;
          if (this.replaceable.set(key, event) !== null) { this.replaceable.clear(); this.scheduler.cancel(); this.stream.requestResync('UI_STATE_BUFFER_OVERFLOW'); return; }
          this.scheduler.schedule(() => { for (const value of this.replaceable.drain()) fanout(value); });
        } else fanout(event);
      },
    });
    this.reconnect = new ReconnectingStream(this.stream, authenticated);
    this.reconnect.observe(status => { const generation = this.generation; for (const member of [...this.members.values()]) { if (generation !== this.generation) return; if (![...this.members.values()].includes(member)) continue; try { member.onStatus?.(this.unready.has(member) ? { ...status, state: 'resync-required', reason: 'CONSUMER_SNAPSHOT_REJECTED' } : { ...status }); } catch (error) { report(member, error); } } });
  }
  subscribe(subscription: CanonicalSubscription): () => void {
    if (this.disposed) throw new Error('STREAM_DISPOSED');
    if (this.authenticated && !this.authorization) throw new Error('AUTH_REQUIRED');
    decodeV2Contract('Subscribe', { op: 'subscribe', subscriptionId: 'browser-topic-policy', topics: subscription.topics, afterCursor: null });
    subscription.signal?.throwIfAborted();
    const member: CanonicalSubscription = { ...subscription, topics: [...subscription.topics] };
    const topics = [...new Set([...this.union, ...member.topics])].sort();
    if (topics.length > 20) throw new TopicConnectionPolicyError('More than20topics requires an explicit separate connection');
    const id = Symbol(); this.members.set(id, member);
    let disposed = false;
    const release = () => {
      if (disposed) return; disposed = true;
      member.signal?.removeEventListener('abort', release); this.detachMembers.delete(id); this.members.delete(id); this.unready.delete(member);
      this.sync();
    };
    this.detachMembers.set(id, () => { disposed = true; member.signal?.removeEventListener('abort', release); });
    member.signal?.addEventListener('abort', release, { once: true });
    if (member.signal?.aborted) { release(); return release; }
    if (this.lastSnapshot && member.topics.every(topic => this.snapshots.has(topic))) {
      try { this.installMemberSnapshot(member, { ...this.lastSnapshot, topics: member.topics.map(topic => this.snapshots.get(topic)!) }); } catch (error) { this.unready.add(member); try { member.onError?.(error); } catch {} }
    }
    if (!this.members.has(id)) return release;
    this.sync();
    try { if (this.members.has(id)) member.onStatus?.({ ...this.reconnect.status }); } catch (error) { try { member.onError?.(error); } catch {} }
    return release;
  }
  private installMemberSnapshot(member: CanonicalSubscription, snapshot: V2TopicSnapshot): void {
    const installed: unknown = member.installSnapshot(structuredClone(snapshot));
    if (installed && typeof installed === 'object' && 'then' in installed) throw new TypeError('Consumer snapshot must commit synchronously');
  }
  private sync() {
    const topics = [...new Set([...this.members.values()].flatMap(member => member.topics))].sort();
    if (topics.length === 0) { this.generation++; this.union = []; this.scheduler.cancel(); this.replaceable.clear(); this.reconnect.stop(); this.snapshots.clear(); this.lastSnapshot = undefined; return; }
    if (JSON.stringify(topics) === JSON.stringify(this.union)) return;
    this.generation++; this.scheduler.cancel(); this.replaceable.clear();
    this.union = topics; void this.reconnect.start(topics);
  }
  private clearMembers(): void { for (const detach of this.detachMembers.values()) detach(); this.detachMembers.clear(); this.members.clear(); }
  setAuthorizationScope(scope: string): void { if (scope !== this.authorization) { this.generation++; this.authorization = scope; this.scheduler.cancel(); this.replaceable.clear(); this.unready.clear(); this.clearMembers(); this.reconnect.logout(); this.union = []; this.snapshots.clear(); this.lastSnapshot = undefined; } }
  retry(): Promise<void> { return this.reconnect.retry(); }
  logout(): void { this.generation++; this.authorization = ''; this.scheduler.cancel(); this.replaceable.clear(); this.unready.clear(); this.clearMembers(); this.union = []; this.snapshots.clear(); this.lastSnapshot = undefined; this.reconnect.logout(); }
  dispose(): void { this.disposed = true; this.generation++; this.scheduler.cancel(); this.replaceable.clear(); this.unready.clear(); this.clearMembers(); this.union = []; this.snapshots.clear(); this.lastSnapshot = undefined; this.reconnect.dispose(); }
  get subscriptionCount(): number { return this.members.size; }
  get topicCount(): number { return this.union.length; }
  get status(): ReconnectStatus { return this.reconnect.status; }
}

const canonicalConnections = new WeakMap<SdkClient, Map<string, { stream: CanonicalDataStream; owners: number }>>();
export function acquireCanonicalDataStream(client: SdkClient, scope: BrowserStreamV2Scope, authenticated: boolean, authorizationScope: string): { stream: CanonicalDataStream; release(): void } {
  let connections = canonicalConnections.get(client);
  if (!connections) { connections = new Map(); canonicalConnections.set(client, connections); }
  const key = JSON.stringify([scope.applicationOrigin, scope.snapshotPath, authenticated, authorizationScope]);
  let entry = connections.get(key);
  if (!entry) { entry = { stream: new CanonicalDataStream(client, scope, authenticated, authorizationScope), owners: 0 }; connections.set(key, entry); }
  entry.owners++;
  const owned = entry; let released = false;
  return { stream: owned.stream, release() { if (released) return; released = true; if (--owned.owners === 0) { owned.stream.dispose(); connections!.delete(key); } } };
}
