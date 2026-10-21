import { decodeContract } from './validate.js';
import { StreamTransportError, decodeV2Contract, type V2ContractTypes } from './transport.js';
import type { StreamEvent, TopicSnapshot, Subscribe, StreamTicketIssueData, Watermark } from './types.gen.js';
export type StreamState = 'idle'|'loading'|'replaying'|'live'|'stale'|'resync-required'|'closed';
export interface StreamTransport {
  snapshot(topics: string[], signal: AbortSignal): Promise<unknown>;
  ticket?(topics: string[], signal: AbortSignal): Promise<unknown>;
  connect(input: { signal: AbortSignal; ticket: StreamTicketIssueData | undefined; receive(value: unknown): void; disconnected(): void }): Promise<{send(frame: Subscribe): void; close(): void}>;
}
export interface SnapshotStreamOptions {
  transport: StreamTransport;
  /** Synchronously install payload and watermarks in one atomic store update. */
  installSnapshot(snapshot: TopicSnapshot): void;
  /** Synchronous store effect. Throwing prevents watermark advancement and requires resync. */
  applyEvent(event: StreamEvent): void;
  /** Notification observers are isolated from committed store effects. */
  onEvent?(event: StreamEvent): void;
  onState?(state: StreamState, reason?: string): void;
  onConsumerError?(error: unknown): void;
  maxBuffer?: number;
}
export class SnapshotStream {
  state: StreamState = 'idle';
  private operation: AbortController | undefined;
  private connection: {send(frame: Subscribe): void; close(): void} | undefined;
  private positions = new Map<string,bigint>();
  private revisions = new Map<string,bigint>();
  private watermarks = new Map<string,Watermark>();
  private buffer: StreamEvent[] = [];
  private topics: string[] = [];
  private cursor: string | undefined;
  private lastCheckpoint = 0;
  private subscriptionId = '';
  private operationNumber = 0;
  private acknowledged = false;
  private readonly maxBuffer: number;
  constructor(private readonly options: SnapshotStreamOptions) {
    this.maxBuffer = options.maxBuffer ?? 10000;
    if (!Number.isSafeInteger(this.maxBuffer) || this.maxBuffer < 1 || this.maxBuffer > 100000) throw new RangeError('Invalid replay buffer bound');
  }
  private consumerError(error: unknown) { try { this.options.onConsumerError?.(error); } catch { /* observer isolated */ } }
  private setState(state: StreamState, reason?: string) {
    this.state = state;
    try { this.options.onState?.(state,reason); } catch(error) { this.consumerError(error); }
  }
  private cancel() {
    this.operation?.abort(); this.operation = undefined;
    this.connection?.close(); this.connection = undefined;
    this.buffer = []; this.acknowledged = false;
  }
  async start(topics: string[], signal?: AbortSignal): Promise<void> {
    this.cancel();
    const subscriptionId = `sdk-${++this.operationNumber}`;
    try { decodeContract('Subscribe',{op:'subscribe',topics,subscriptionId,afterCursor:null}); }
    catch (error) { this.fail(error instanceof Error ? error.message : 'Invalid subscription'); return; }
    this.topics = [...topics]; this.subscriptionId = subscriptionId;
    this.cursor = undefined; this.positions.clear(); this.revisions.clear(); this.watermarks.clear();
    const controller = new AbortController(); this.operation = controller;
    const abort = () => { if (this.operation === controller) { this.cancel(); this.setState('closed'); } };
    signal?.addEventListener('abort',abort,{once:true});
    controller.signal.addEventListener('abort',() => signal?.removeEventListener('abort',abort),{once:true});
    if (signal?.aborted) { abort(); return; }
    this.setState('loading');
    const current = () => this.operation === controller && !controller.signal.aborted;
    try {
      const snapshot = decodeContract('TopicSnapshot',await this.options.transport.snapshot(this.topics,controller.signal));
      if (!current()) return;
      if (snapshot.topics.length !== topics.length || new Set(snapshot.topics.map(t=>t.topic)).size !== topics.length || snapshot.topics.some(t=>!topics.includes(t.topic))) throw new Error('Snapshot topic mismatch');
      for (const entry of snapshot.topics) decodeContract('StreamEvent',{...entry,eventId:'snapshot-validation',occurredAt:snapshot.generatedAt,coalesced:false,sourceSequenceFirst:null,sourceSequenceLast:null});
      if (snapshot.topics.some(t=>t.watermark?.integrity === 'quarantined')) throw new Error('Snapshot integrity quarantined');
      const positions = new Map(snapshot.topics.map(t=>[t.topic,BigInt(t.sequence)]));
      this.options.installSnapshot(snapshot);
      if (!current()) return;
      this.positions = positions;
      this.revisions = new Map(snapshot.topics.map(t=>[t.topic,BigInt(t.revision)]));
      this.watermarks = new Map(snapshot.topics.filter(t=>t.watermark !== null).map(t=>[t.topic,t.watermark!]));
      this.cursor = snapshot.resumeCursor;
      this.setState('replaying');
      const rawTicket = this.options.transport.ticket ? await this.options.transport.ticket(this.topics,controller.signal) : undefined;
      if (!current()) return;
      const ticket = rawTicket === undefined ? undefined : decodeContract('StreamTicketIssueData',rawTicket);
      if (ticket && (Date.parse(ticket.expiresAt) <= Date.now() || Date.parse(ticket.expiresAt) > Date.now()+30000)) throw new Error('Invalid ticket lifetime');
      const connection = await this.options.transport.connect({signal:controller.signal,ticket,
        receive: value => { if (current()) this.receive(value); },
        disconnected: () => { if (current()) { this.cancel(); this.setState('stale','transport closed'); } },
      });
      if (!current()) { connection.close(); return; }
      this.connection = connection;
      connection.send(decodeContract('Subscribe',{op:'subscribe',subscriptionId,topics:this.topics,afterCursor:this.cursor}));
    } catch (error) { if (current()) this.fail(error instanceof Error ? error.message : 'snapshot/resume failed'); }
  }
  private fail(reason: string) { this.cancel(); this.cursor = undefined; this.setState('resync-required',reason); }
  private receive(value: unknown) {
    const operation = this.operation;
    try {
      if (value && typeof value === 'object' && 'op' in value) {
        const control = decodeContract('StreamControl',value);
        if (control.watermark?.integrity === 'quarantined') throw new Error('Checkpoint integrity quarantined');
        if (control.subscriptionId !== null && control.subscriptionId !== this.subscriptionId) throw new Error('Foreign subscription control');
        if (['resnapshot_required','integrity_incident','subscription_closed','error'].includes(control.op)) { this.fail(control.code ?? control.op); return; }
        if (control.op === 'subscribed') { this.acknowledged = true; return; }
        if (control.op === 'checkpoint') {
          if (!this.acknowledged || !control.cursor) throw new Error('Checkpoint before acknowledgement');
          const buffered = this.buffer; this.buffer = [];
          for (const event of buffered) { if (this.operation !== operation) return; this.apply(event); }
          if (this.operation !== operation) return;
          this.cursor = control.cursor; this.lastCheckpoint = Date.now(); this.setState('live');
        }
        return;
      }
      const event = decodeContract('StreamEvent',value);
      if (!this.positions.has(event.topic)) throw new Error('Unrequested event topic');
      if (this.state === 'replaying') {
        if (this.buffer.length >= this.maxBuffer) throw new Error('Replay buffer exceeded; durable resnapshot required');
        this.buffer.push(event);
      } else if (this.state === 'live') this.apply(event);
    } catch(error) { if (this.operation === operation) this.fail(error instanceof Error ? error.message : 'Invalid stream envelope'); }
  }
  private apply(event: StreamEvent) {
    const previous = this.positions.get(event.topic)!;
    const next = BigInt(event.sequence);
    if (next <= previous) return;
    if (next !== previous+1n) {
      const first = event.sourceSequenceFirst, last = event.sourceSequenceLast;
      if (!event.coalesced || first === null || last === null || BigInt(first) !== previous+1n || BigInt(last) !== next || event.topic.startsWith('market.trades.')) throw new Error('Sequence gap requires durable reconciliation');
    }
    if (event.watermark?.integrity === 'quarantined') throw new Error('Projection integrity quarantined');
    if (BigInt(event.revision) < (this.revisions.get(event.topic) ?? 0n)) throw new Error('Projection revision regression');
    const previousWatermark = this.watermarks.get(event.topic), nextWatermark = event.watermark;
    if (previousWatermark && nextWatermark) {
      const before = BigInt(previousWatermark.height), after = BigInt(nextWatermark.height);
      if (after < before || (after === before && previousWatermark.blockHash.toLowerCase() !== nextWatermark.blockHash.toLowerCase()) || BigInt(nextWatermark.projectionRevision) < BigInt(previousWatermark.projectionRevision)) throw new Error('Committed watermark regression or integrity conflict');
    }
    const operation = this.operation;
    this.options.applyEvent(event);
    if (this.operation !== operation) return;
    this.revisions.set(event.topic,BigInt(event.revision));
    if (nextWatermark) this.watermarks.set(event.topic,nextWatermark);
    this.positions.set(event.topic,next);
    try { this.options.onEvent?.(event); } catch(error) { this.consumerError(error); }
  }
  /** Always refresh after visibility restore; no stale opaque cursor assumptions. */
  resumeVisible(): Promise<void> {
    this.setState('stale',Date.now()-this.lastCheckpoint >= 72*3600*1000 ? 'resume cursor expired' : 'visibility restored');
    return this.start(this.topics);
  }
  logout(): void { this.cancel(); this.cursor=undefined; this.positions.clear(); this.revisions.clear(); this.watermarks.clear(); this.topics=[]; this.setState('closed','authorization changed'); }
  close(): void { this.logout(); }
}

export type V2StreamEvent = V2ContractTypes['StreamEvent'];
export type V2TopicSnapshot = V2ContractTypes['TopicSnapshot'];
type V2Subscribe = V2ContractTypes['Subscribe'];
type V2StreamTicketIssueData = V2ContractTypes['StreamTicketIssueData'];
type V2Watermark = V2ContractTypes['Watermark'];

export type StreamStateV2 = 'idle'|'loading'|'replaying'|'live'|'stale'|'resync-required'|'closed';
export interface StreamTransportV2 {
  snapshot(topics: string[], signal: AbortSignal): Promise<unknown>;
  ticket?(topics: string[], signal: AbortSignal): Promise<unknown>;
  connect(input: { signal: AbortSignal; ticket: V2StreamTicketIssueData | undefined; receive(value: unknown): void; disconnected(): void }): Promise<{send(frame: V2Subscribe): void; close(): void}>;
}
export interface SnapshotStreamV2Options {
  transport: StreamTransportV2;
  /** Synchronously install payload and watermarks in one atomic store update. */
  installSnapshot(snapshot: V2TopicSnapshot): void;
  /** Synchronous store effect. Throwing prevents watermark advancement and requires resync. */
  applyEvent(event: V2StreamEvent): void;
  /** Notification observers are isolated from committed store effects. */
  onEvent?(event: V2StreamEvent): void;
  onState?(state: StreamStateV2, reason?: string): void;
  onConsumerError?(error: unknown): void;
  maxBuffer?: number;
}
function canonicalV2Payload(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(entry => canonicalV2Payload(entry)).join(',')}]`;
  if (value !== null && typeof value === 'object') return `{${Object.entries(value).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([key, entry]) => `${JSON.stringify(key)}:${canonicalV2Payload(entry)}`).join(',')}}`;
  const encoded = JSON.stringify(value);
  if (encoded === undefined) throw new Error('Non-JSON projection payload');
  return encoded;
}
function v2ProjectionIdentity(topic: string, payload: V2StreamEvent['payload'], revision: string) {
  if (topic.startsWith('market.trades.')) { const trade = decodeV2Contract('Trade', payload); return { key: `${topic}:${trade.eventId}`, revision: BigInt(revision) }; }
  if (topic.startsWith('private.notifications.')) {
    const notification = decodeV2Contract('Notification', payload);
    return { key: `${topic}:${notification.notificationId}`, revision: BigInt(revision) };
  }
  if (topic.startsWith('market.candles.')) {
    const candle = decodeV2Contract('Candle', payload);
    return { key: `${topic}:${candle.start}:${candle.seriesModelVersion}`, revision: BigInt(candle.revision) };
  }
  return { key: topic, revision: BigInt(revision) };
}

export class SnapshotStreamV2 {
  state: StreamStateV2 = 'idle';
  lastError: unknown;
  private readonly stateObservers = new Set<(state: StreamStateV2, reason?: string) => void>();
  observeState(observer: (state: StreamStateV2, reason?: string) => void): () => void { this.stateObservers.add(observer); return () => { this.stateObservers.delete(observer); }; }
  get checkpointAt(): number { return this.lastCheckpoint; }
  get hasResumeCursor(): boolean { return this.cursor !== undefined; }
  requestResync(reason: string): void { this.fail(reason); }
  private operation: AbortController | undefined;
  private connection: {send(frame: V2Subscribe): void; close(): void} | undefined;
  private positions = new Map<string,bigint>();
  private revisions = new Map<string,bigint>();
  private revisionContents = new Map<string,string>();
  private topicRevisions = new Map<string, { revision: bigint; content: string }>();
  private watermarks = new Map<string,V2Watermark>();
  private buffer: V2StreamEvent[] = [];
  private topics: string[] = [];
  private cursor: string | undefined;
  private lastCheckpoint = 0;
  private resumeIssuedAt = 0;
  private subscriptionId = '';
  private operationNumber = 0;
  private acknowledged = false;
  private authorizationSignal: AbortSignal | undefined;
  private readonly maxBuffer: number;
  constructor(private readonly options: SnapshotStreamV2Options) {
    this.maxBuffer = options.maxBuffer ?? 10000;
    if (!Number.isSafeInteger(this.maxBuffer) || this.maxBuffer < 1 || this.maxBuffer > 100000) throw new RangeError('Invalid replay buffer bound');
  }
  private consumerError(error: unknown) { try { this.options.onConsumerError?.(error); } catch { /* observer isolated */ } }
  private setState(state: StreamStateV2, reason?: string) {
    const operation = this.operation;
    this.state = state;
    try { this.options.onState?.(state,reason); } catch(error) { this.consumerError(error); }
    for (const observer of [...this.stateObservers]) { if (operation !== this.operation || this.state !== state) return; if (!this.stateObservers.has(observer)) continue; try { observer(state, reason); } catch (error) { this.consumerError(error); } }
  }
  private cancel() {
    this.operation?.abort(); this.operation = undefined;
    this.connection?.close(); this.connection = undefined;
    this.buffer = []; this.acknowledged = false;
  }
  async start(topics: string[], signal?: AbortSignal, resume = false): Promise<void> {
    resume = resume && signal === this.authorizationSignal && JSON.stringify([...topics].sort()) === JSON.stringify([...this.topics].sort()) && this.cursor !== undefined && this.lastCheckpoint > 0 && Date.now() - this.resumeIssuedAt < 72*3600*1000;
    this.cancel();
    this.authorizationSignal = signal;
    const subscriptionId = `sdk-${++this.operationNumber}`;
    try { decodeV2Contract('Subscribe',{op:'subscribe',topics,subscriptionId,afterCursor:null}); }
    catch (error) { this.fail(error instanceof Error ? error.message : 'Invalid subscription'); return; }
    this.topics = [...topics]; this.subscriptionId = subscriptionId;
    if (!resume) { this.cursor = undefined; this.positions.clear(); this.revisions.clear(); this.revisionContents.clear(); this.topicRevisions.clear(); this.watermarks.clear(); }
    const controller = new AbortController(); this.operation = controller;
    const abort = () => { if (this.operation === controller) this.logout(); };
    signal?.addEventListener('abort',abort,{once:true});
    controller.signal.addEventListener('abort',() => signal?.removeEventListener('abort',abort),{once:true});
    if (signal?.aborted) { abort(); return; }
    this.lastError = undefined;
    const current = () => this.operation === controller && !controller.signal.aborted;
    this.setState(resume ? 'replaying' : 'loading');
    if (!current()) return;
    try {
      if (!resume) {
      const snapshot = decodeV2Contract('TopicSnapshot',await this.options.transport.snapshot(this.topics,controller.signal));
      if (!current()) return;
      if (snapshot.topics.length !== topics.length || new Set(snapshot.topics.map(t=>t.topic)).size !== topics.length || snapshot.topics.some(t=>!topics.includes(t.topic))) throw new Error('Snapshot topic mismatch');
      for (const entry of snapshot.topics) decodeV2Contract('StreamEvent',{...entry,eventId:'snapshot-validation',occurredAt:snapshot.generatedAt,coalesced:false,sourceSequenceFirst:null,sourceSequenceLast:null});
      if (snapshot.topics.some(t=>t.watermark?.integrity === 'quarantined')) throw new Error('Snapshot integrity quarantined');
      const positions = new Map(snapshot.topics.map(t=>[t.topic,BigInt(t.sequence)]));
      const installed: unknown = this.options.installSnapshot(structuredClone(snapshot));
      if (installed && typeof installed === 'object' && 'then' in installed) throw new TypeError('Snapshot store effect must commit synchronously');
      if (!current()) return;
      this.positions = positions;
      this.revisions.clear(); this.revisionContents.clear(); this.topicRevisions.clear();
      for (const entry of snapshot.topics) {
        const identity = v2ProjectionIdentity(entry.topic, entry.payload, entry.revision);
        this.revisions.set(identity.key, identity.revision);
        const content = canonicalV2Payload(entry.payload);
        this.revisionContents.set(identity.key, content);
        this.topicRevisions.set(entry.topic, { revision: BigInt(entry.revision), content });
      }
      this.watermarks = new Map(snapshot.topics.filter(t=>t.watermark !== null).map(t=>[t.topic,t.watermark!]));
      this.cursor = snapshot.resumeCursor; this.resumeIssuedAt = Date.parse(snapshot.generatedAt);
      }
      this.setState('replaying');
      if (!current()) return;
      const rawTicket = this.options.transport.ticket ? await this.options.transport.ticket(this.topics,controller.signal) : undefined;
      if (!current()) return;
      const ticket = rawTicket === undefined ? undefined : decodeV2Contract('StreamTicketIssueData',rawTicket);
      if (ticket && (Date.parse(ticket.expiresAt) <= Date.now() || Date.parse(ticket.expiresAt) > Date.now()+30000)) throw new Error('Invalid ticket lifetime');
      const connection = await this.options.transport.connect({signal:controller.signal,ticket,
        receive: value => { if (current()) this.receive(value); },
        disconnected: () => { if (current()) { this.cancel(); this.setState('stale','transport closed'); } },
      });
      if (!current()) { connection.close(); return; }
      this.connection = connection;
      connection.send(decodeV2Contract('Subscribe',{op:'subscribe',subscriptionId,topics:this.topics,afterCursor:this.cursor}));
    } catch (error) { if (current()) { this.lastError = error; this.cancel(); this.setState('stale', error instanceof Error ? error.message : 'snapshot/resume failed'); } }
  }
  async reconnect(signal?: AbortSignal): Promise<void> {
    const resume = this.cursor !== undefined && this.lastCheckpoint > 0 && Date.now() - this.resumeIssuedAt < 72*60*60*1000;
    return this.start([...this.topics], signal ?? this.authorizationSignal, resume);
  }
  private fail(reason: string) { this.lastError = new StreamTransportError(reason); this.cancel(); this.cursor = undefined; this.setState('resync-required',reason); }
  private receive(value: unknown) {
    const operation = this.operation;
    try {
      if (value && typeof value === 'object' && 'schemaMajor' in value && value.schemaMajor !== 2) throw new Error('UNSUPPORTED_VERSION');
      if (value && typeof value === 'object' && 'op' in value) {
        const control = decodeV2Contract('StreamControl',value);
        if (control.watermark?.integrity === 'quarantined') throw new Error('Checkpoint integrity quarantined');
        if (control.subscriptionId !== null && control.subscriptionId !== this.subscriptionId) throw new Error('Foreign subscription control');
        if (['resnapshot_required','integrity_incident','subscription_closed','error'].includes(control.op)) { this.fail(control.code ?? control.op); return; }
        if ((control.op === 'subscribed' || control.op === 'checkpoint') && control.subscriptionId !== this.subscriptionId) throw new Error('Unbound subscription acknowledgement');
        if (control.op === 'unsubscribed') { this.fail('Subscription ended'); return; }
        if (control.op === 'subscribed') { this.acknowledged = true; return; }
        if (control.op === 'checkpoint') {
          if (!this.acknowledged || !control.cursor) throw new Error('Checkpoint before acknowledgement');
          const buffered = this.buffer; this.buffer = [];
          for (const event of buffered) { if (this.operation !== operation) return; this.apply(event); }
          if (this.operation !== operation) return;
          this.cursor = control.cursor; this.lastCheckpoint = Date.now(); this.setState('live');
        }
        return;
      }
      const event = decodeV2Contract('StreamEvent',value);
      if (!this.positions.has(event.topic)) throw new Error('Unrequested event topic');
      if (this.state === 'replaying') {
        if (this.buffer.length >= this.maxBuffer) throw new Error('Replay buffer exceeded; durable resnapshot required');
        this.buffer.push(event);
      } else if (this.state === 'live') this.apply(event);
    } catch(error) { if (this.operation === operation) this.fail(error instanceof Error ? error.message : 'Invalid stream envelope'); }
  }
  private apply(event: V2StreamEvent) {
    const previous = this.positions.get(event.topic)!;
    const next = BigInt(event.sequence);
    const identity = v2ProjectionIdentity(event.topic, event.payload, event.revision);
    const revision = this.revisions.get(identity.key);
    const content = canonicalV2Payload(event.payload);
    if (revision === identity.revision && this.revisionContents.get(identity.key) !== content) throw new Error('Conflicting same-revision projection payload');
    if (next <= previous) return;
    if (revision === undefined && this.revisions.size >= Math.min(this.maxBuffer, 10000)) throw new Error('Projection revision retention exhausted; resnapshot required');
    if (next !== previous+1n) {
      const first = event.sourceSequenceFirst, last = event.sourceSequenceLast;
      if (!event.coalesced || first === null || last === null || BigInt(first) !== previous+1n || BigInt(last) !== next || event.topic.startsWith('market.trades.')) throw new Error('Sequence gap requires durable reconciliation');
    }
    if (event.watermark?.integrity === 'quarantined') throw new Error('Projection integrity quarantined');
    const topicRevision = this.topicRevisions.get(event.topic);
    const incomingTopicRevision = BigInt(event.revision);
    if (topicRevision && incomingTopicRevision < topicRevision.revision) throw new Error('Projection revision regression');
    if (topicRevision && incomingTopicRevision === topicRevision.revision && content !== topicRevision.content) throw new Error('Conflicting same-revision topic projection');
    const newer = revision === undefined || identity.revision > revision;
    const previousV2Watermark = this.watermarks.get(event.topic), nextV2Watermark = event.watermark;
    if (previousV2Watermark && nextV2Watermark) {
      const before = BigInt(previousV2Watermark.height), after = BigInt(nextV2Watermark.height);
      if (after < before || (after === before && previousV2Watermark.blockHash.toLowerCase() !== nextV2Watermark.blockHash.toLowerCase()) || BigInt(nextV2Watermark.projectionRevision) < BigInt(previousV2Watermark.projectionRevision)) throw new Error('Committed watermark regression or integrity conflict');
    }
    const operation = this.operation;
    if (newer) { const applied: unknown = this.options.applyEvent(structuredClone(event)); if (applied && typeof applied === 'object' && 'then' in applied) throw new TypeError('Projection store effect must commit synchronously'); }
    if (this.operation !== operation) return;
    if (newer) { this.revisions.set(identity.key, identity.revision); this.revisionContents.set(identity.key, content); }
    this.topicRevisions.set(event.topic, { revision: incomingTopicRevision, content });
    if (nextV2Watermark) this.watermarks.set(event.topic,nextV2Watermark);
    this.positions.set(event.topic,next);
    try { if (newer) this.options.onEvent?.(structuredClone(event)); } catch(error) { this.consumerError(error); }
  }
  /** Always refresh after visibility restore; no stale opaque cursor assumptions. */
  resumeVisible(): Promise<void> {
    const topics = [...this.topics], signal = this.authorizationSignal, operation = this.operation;
    if (!topics.length || signal?.aborted || this.state === 'closed') return Promise.resolve();
    this.setState('stale',Date.now()-this.resumeIssuedAt >= 72*3600*1000 ? 'resume cursor expired' : 'visibility restored');
    if (operation !== this.operation || signal?.aborted || !this.topics.length) return Promise.resolve();
    return this.start(topics, signal);
  }
  logout(): void { this.cancel(); this.cursor=undefined; this.positions.clear(); this.revisions.clear(); this.revisionContents.clear(); this.topicRevisions.clear(); this.watermarks.clear(); this.topics=[]; this.authorizationSignal=undefined; this.resumeIssuedAt=0; this.lastCheckpoint=0; this.setState('closed','authorization changed'); }
  close(): void { this.logout(); }
}
