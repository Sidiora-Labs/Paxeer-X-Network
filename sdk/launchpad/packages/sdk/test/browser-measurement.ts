import { CanonicalDataStream } from '../../../../../apps/launchpad/web/src/core/realtime/data-stream';
import { SdkClient, SnapshotStreamV2, ReconnectingStream, browserStreamTransportV2, type TransportConfig, type V2StreamEvent, type V2TopicSnapshot } from '../src/index';
import { createElement, StrictMode } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { useCanonicalDataStream } from '../../../../../apps/launchpad/web/src/core/realtime/use-data-stream';

type Input = { transport: TransportConfig; applicationOrigin: string; accessToken: string; publicTopics: string[]; privateTopics: string[]; tradeTopics: string[]; candleTopics: string[]; authorizationScope: string; maximumEvents: number; maximumFrames: number };
let publicStream: CanonicalDataStream | undefined;
let privateStream: SnapshotStreamV2 | undefined;
let privateConnection: ReconnectingStream | undefined;
let root: Root | undefined;
let element: HTMLDivElement | undefined;
let publicRelease: (() => void) | undefined;
let input: Input;
let frame = 0;
let frameHandle: number | undefined;
let recording = false;
let privateDeliveries = 0;
let publicSnapshots = 0;
let observerErrors = 0;
let candleCallbacks = 0;
let failure: string | undefined;
const arrivals: number[] = [];
const events = new Map<string, V2StreamEvent>();
const latest = new Map<string, V2TopicSnapshot['topics'][number]>();
const frames: { index: number; at: number }[] = [];
const requests: { url: string; startedAt: number; finishedAt?: number; status?: number; aborted?: boolean }[] = [];
const secrets = new Set<string>();
const trackedFetch: typeof fetch = async (resource, options) => {
  const url = new URL(resource instanceof Request ? resource.url : String(resource), location.href);
  if ([...url.searchParams.keys()].some(key => /^(?:token|ticket|jwt|authorization|access_token|refresh_token)$/i.test(key))) throw new Error('URL_CREDENTIAL');
  const entry: typeof requests[number] = { url: url.toString(), startedAt: performance.now() };
  if (requests.length >= 10000) throw new Error('REQUEST_CAPTURE_BOUND');
  requests.push(entry);
  try {
    const response = await fetch(resource, options);
    entry.status = response.status;
    if (url.pathname === '/v2/stream/tickets' && response.ok) {
      const value = await response.clone().json() as { data?: { ticket?: unknown } };
      if (typeof value.data?.ticket === 'string') secrets.add(value.data.ticket);
    }
    return response;
  } finally { entry.finishedAt = performance.now(); entry.aborted = options?.signal?.aborted ?? false; }
};
function fail(code: string): never { failure = code; throw new Error(code); }
function install(snapshot: V2TopicSnapshot): void {
  publicSnapshots++;
  latest.clear();
  for (const topic of snapshot.topics) latest.set(topic.topic, structuredClone(topic));
}
function apply(event: V2StreamEvent): void {
  latest.set(event.topic, { topic: event.topic, payload: structuredClone(event.payload), sequence: event.sequence, revision: event.revision, watermark: event.watermark });
  if (!event.topic.startsWith('market.trades.')) return;
  if (events.has(event.eventId)) fail('DUPLICATE_IMMUTABLE_EVENT');
  if (events.size >= input.maximumEvents) fail('EVENT_CAPTURE_BOUND');
  if (recording) { arrivals.push(performance.now()); performance.mark(`L24.event.${arrivals.length - 1}`); }
  events.set(event.eventId, structuredClone(event));
}

function Observer() {
  if (!publicStream) throw new Error('STREAM_UNAVAILABLE');
  useCanonicalDataStream(publicStream, { topics: input.candleTopics, installSnapshot: () => {}, onEvent: () => { candleCallbacks++; throw new Error('ISOLATED_CANDLE_OBSERVER'); }, onError: () => { observerErrors++; } });
  return createElement('output', { role: 'status', 'aria-label': 'Stream observer' }, 'Market observer active');
}
function tick(at: number): void {
  if (!recording) return;
  if (frames.length >= input.maximumFrames) { failure = 'FRAME_CAPTURE_BOUND'; recording = false; return; }
  frames.push({ index: frame, at });
  performance.mark(`L24.frame.${frame++}`);
  frameHandle = requestAnimationFrame(tick);
}
async function start(config: Input): Promise<void> {
  dispose(); input = config;
  if (location.origin !== config.applicationOrigin || !Array.isArray(config.publicTopics) || !config.publicTopics.length || !Array.isArray(config.privateTopics) || !config.privateTopics.length || !config.accessToken) fail('REAL_BROWSER_SCOPE_REQUIRED');
  if (!Number.isSafeInteger(config.maximumEvents) || config.maximumEvents < 100 || config.maximumEvents > 1000000 || !Number.isSafeInteger(config.maximumFrames) || config.maximumFrames < 100 || config.maximumFrames > 1000000) fail('INVALID_CAPTURE_BUDGET');
  events.clear(); latest.clear(); frames.length = 0; requests.length = 0; secrets.clear(); secrets.add(config.accessToken);
  arrivals.length = 0; frame = 0; privateDeliveries = 0; publicSnapshots = 0; observerErrors = 0; candleCallbacks = 0; failure = undefined;
  const client = new SdkClient({ ...config.transport, session: { mode: 'bearer', token: () => config.accessToken } }, trackedFetch);
  publicStream = new CanonicalDataStream(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, false, '');
  publicRelease = publicStream.subscribe({ topics: config.publicTopics, installSnapshot: install, onEvent: apply, onError: () => { failure = 'PUBLIC_CONSUMER_FAILED'; } });
  privateStream = new SnapshotStreamV2({ transport: browserStreamTransportV2(client, { applicationOrigin: config.applicationOrigin, snapshotPath: '/v2/snapshots' }, true), installSnapshot: () => {}, applyEvent: () => { privateDeliveries++; } });
  privateConnection = new ReconnectingStream(privateStream, true);
  await privateConnection.start(config.privateTopics);
  element = document.createElement('div'); element.dataset.l24Measurement = 'observer'; document.body.append(element);
  root = createRoot(element); root.render(createElement(StrictMode, null, createElement(Observer)));
}
function begin(): void { if (recording) fail('MEASUREMENT_ALREADY_RUNNING'); recording = true; performance.mark('L24.workload.begin'); frameHandle = requestAnimationFrame(tick); }
function end(): void { recording = false; if (frameHandle !== undefined) cancelAnimationFrame(frameHandle); frameHandle = undefined; performance.mark('L24.workload.end'); }
async function retry(): Promise<void> { if (!publicStream) fail('STREAM_UNAVAILABLE'); await publicStream.retry(); }
async function restore(): Promise<void> {
  if (!publicStream || !privateConnection) fail('STREAM_UNAVAILABLE');
  publicRelease?.();
  publicRelease = publicStream.subscribe({ topics: input.publicTopics, installSnapshot: install, onEvent: apply, onError: () => { failure = 'PUBLIC_CONSUMER_FAILED'; } });
  await privateConnection.restoreVisibility();
}
async function remount(): Promise<void> {
  root?.unmount(); if (!element) fail('OBSERVER_UNAVAILABLE');
  root = createRoot(element); root.render(createElement(StrictMode, null, createElement(Observer)));
}
function state() { return { failure, publicStatus: publicStream?.status, privateStatus: privateConnection?.status, privateHasCursor: privateStream?.hasResumeCursor, privateDeliveries, publicSnapshots, observerErrors, candleCallbacks, subscriptions: publicStream?.subscriptionCount, frames: structuredClone(frames), arrivals: [...arrivals], requests: structuredClone(requests), events: [...events.values()], latest: [...latest.values()], device: { userAgent: navigator.userAgent, hardwareConcurrency: navigator.hardwareConcurrency, deviceMemory: (navigator as Navigator & { deviceMemory?: number }).deviceMemory, width: screen.width, height: screen.height, devicePixelRatio } }; }
function dispose(): { subscriptions: number; topics: number } { end(); root?.unmount(); root = undefined; element?.remove(); element = undefined; publicRelease?.(); publicRelease = undefined; publicStream?.dispose(); const subscriptions = publicStream ? publicStream.subscriptionCount : 0, topics = publicStream ? publicStream.topicCount : 0; publicStream = undefined; privateConnection?.dispose(); privateConnection = undefined; privateStream = undefined; return { subscriptions, topics }; }
Object.assign(globalThis, { L24Measurement: { start, begin, end, retry, restore, remount, state, dispose, sensitiveValues: () => [...secrets] } });
