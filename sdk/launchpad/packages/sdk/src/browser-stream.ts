import { PublicSnapshotRequests, snapshotRequestKey } from './shared-snapshot.js';
import { decodeContract } from './validate.js';
import { SdkClient, decodeV2Contract, StreamTransportError } from './transport.js';
import type { StreamTransport, StreamTransportV2 } from './stream.js';
/** Actual browser HTTP + WebSocket adapter; credentials never enter URLs. */
export function browserStreamTransport(client: SdkClient, authenticated = false): StreamTransport {
  return {
    snapshot: async (topics,signal) => {
      decodeContract('TopicSnapshotReadRequest',{topics});
      const response = await client.request(`/v1/snapshots?topics=${encodeURIComponent(topics.join(','))}`,'TopicSnapshotReadResponse',{signal,authenticated,idempotentRead:true});
      return response.data.snapshot;
    },
    ...(authenticated ? {ticket: async (topics: string[],signal: AbortSignal) => {
      const body = decodeContract('StreamTicketIssueBody',{topics,schemaMajor:1});
      return (await client.request('/v1/stream-tickets','StreamTicketIssueResponse',{method:'POST',body,signal,authenticated:true})).data;
    }} : {}),
    connect: ({signal,ticket,receive,disconnected}) => new Promise((resolve,reject) => {
      signal.throwIfAborted();
      if (authenticated && !ticket) { reject(new Error('Fresh stream ticket required')); return; }
      if (ticket && !/^[A-Za-z0-9_.-]+$/.test(ticket.ticket)) { reject(new Error('Invalid subprotocol ticket')); return; }
      const protocols = ticket ? ['sidiora.v1',`ticket.${ticket.ticket}`] : ['sidiora.v1'];
      const socket = new WebSocket(new URL('/v1/stream',client.config.streamOrigin),protocols);
      let opened = false;
      const abort = () => { socket.close(); reject(signal.reason); };
      signal.addEventListener('abort',abort,{once:true});
      socket.onopen = () => {
        if (signal.aborted) { socket.close(); return; }
        if (socket.protocol !== 'sidiora.v1') { reject(new Error('Unsupported stream version')); socket.close(); return; }
        opened = true;
        resolve({send: frame => socket.send(JSON.stringify(frame)),close: () => { signal.removeEventListener('abort',abort); socket.onclose=null; socket.onmessage=null; socket.close(); }});
      };
      socket.onmessage = event => {
        if (signal.aborted) return;
        try { receive(JSON.parse(event.data)); } catch { socket.close(1007,'Invalid stream envelope'); }
      };
      socket.onerror = () => { if (!opened) reject(new Error('Stream transport failed')); };
      socket.onclose = () => { signal.removeEventListener('abort',abort); if (!opened) reject(new Error('Stream closed before handshake')); else if (!signal.aborted) disconnected(); };
    }),
  };
}


const publicSnapshotClients = new WeakMap<SdkClient, PublicSnapshotRequests>();
export interface BrowserStreamV2Scope {
  applicationOrigin: string;
  snapshotPath: string;
}
/**
 * Client liveness probe for WS /v2/stream. core/api answers a text frame
 * {"op":"ping"} with one StreamControl heartbeat (internal/ws/authenticated.go)
 * and never sends heartbeats on its own, so an idle but healthy subscription
 * is only observable through these replies. The server closes a connection
 * that sends more than 10 actions per second (broker.ActionLimit); one ping
 * every 4 s stays far below that.
 */
export const STREAM_PING_FRAME = '{"op":"ping"}';
export const STREAM_PING_INTERVAL_MS = 4000;
export interface BrowserStreamV2Options {
  /** Ping cadence after subscribe; bounded to [1000, 25000] ms (server read deadline is 30 s). */
  pingIntervalMs?: number;
}
export function browserStreamTransportV2(client: SdkClient, scope: BrowserStreamV2Scope, authenticated = true, options: BrowserStreamV2Options = {}): StreamTransportV2 {
  if (!/^\/v2\/[A-Za-z0-9_/-]+$/.test(scope.snapshotPath)) throw new TypeError('Actual v2 snapshot endpoint required');
  const pingIntervalMs = options.pingIntervalMs ?? STREAM_PING_INTERVAL_MS;
  if (!Number.isSafeInteger(pingIntervalMs) || pingIntervalMs < 1000 || pingIntervalMs > 25000) throw new RangeError('Invalid stream ping interval');
  const application = new URL(scope.applicationOrigin);
  // The application origin is bound into stream tickets, so ticketed (authenticated)
  // and production streams require HTTPS. A public development stream never
  // issues a ticket and may run on a plain-http dev origin.
  if ((application.protocol !== 'https:' && (authenticated || client.config.production || application.protocol !== 'http:')) || application.origin !== scope.applicationOrigin) throw new TypeError('Exact HTTPS application origin required');
  return {
    snapshot: async (topics, signal) => {
      decodeV2Contract('TopicSnapshotReadRequest', { topics });
      if (!authenticated && topics.some(topic => topic.startsWith('private.'))) throw new StreamTransportError('AUTH_REQUIRED', 401);
      const load = (requestSignal: AbortSignal) => client.requestV2(`${scope.snapshotPath}?topics=${encodeURIComponent([...topics].sort().join(','))}`, 'TopicSnapshotReadResponse', { signal: requestSignal, authenticated, idempotentRead: true });
      let cache = publicSnapshotClients.get(client);
      if (!cache) { cache = new PublicSnapshotRequests(); publicSnapshotClients.set(client, cache); }
      const response = authenticated ? await load(signal) : await cache.request(snapshotRequestKey(client.config.apiOrigin, scope.snapshotPath, { topics: [...topics].sort() }), load, signal);
      if (response.quality === 'quarantined') throw new StreamTransportError('INTEGRITY_QUARANTINED');
      return response.data.snapshot;
    },
    ...(authenticated ? { ticket: async (topics: string[], signal: AbortSignal) => {
      const body = decodeV2Contract('StreamTicketIssueBody', { topics, schemaMajor: 2, filterDigest: await streamTopicFilterDigest(topics), origin: application.origin, endpoint: '/v2/stream' });
      return (await client.requestV2('/v2/stream/tickets', 'StreamTicketIssueResponse', { method: 'POST', body, signal, authenticated: true, service: 'identity' })).data;
    } } : {}),
    connect: ({ signal, ticket, receive, disconnected }) => new Promise((resolve, reject) => {
      signal.throwIfAborted();
      if (authenticated && !ticket) { reject(new StreamTransportError('AUTH_EXPIRED', 401)); return; }
      if (ticket && (!/^[0-9a-f]{64}$/.test(ticket.ticket) || ticket.subprotocol !== 'sidiora.v2' || Date.parse(ticket.expiresAt) <= Date.now())) { reject(new StreamTransportError('AUTH_EXPIRED', 401)); return; }
      const socket = new WebSocket(new URL('/v2/stream', client.config.streamOrigin), ticket ? ['sidiora.v2', `ticket.${ticket.ticket}`] : ['sidiora.v2']);
      let opened = false;
      let ping: ReturnType<typeof setInterval> | undefined;
      const stopPing = () => { if (ping !== undefined) { clearInterval(ping); ping = undefined; } };
      // Heartbeat requests start once the subscribe frame is on the wire (the
      // server reads no client action before it) and stop with the connection.
      const startPing = () => {
        if (ping !== undefined) return;
        ping = setInterval(() => {
          if (signal.aborted || socket.readyState !== WebSocket.OPEN) { stopPing(); return; }
          try { socket.send(STREAM_PING_FRAME); } catch { stopPing(); }
        }, pingIntervalMs);
      };
      const timer = setTimeout(() => { detach(); socket.close(); reject(new StreamTransportError('HANDSHAKE_TIMEOUT')); }, 10000);
      const detach = () => { clearTimeout(timer); stopPing(); signal.removeEventListener('abort', abort); socket.onmessage = null; socket.onclose = null; socket.onerror = null; socket.onopen = null; };
      const abort = () => { detach(); socket.close(); reject(signal.reason); };
      signal.addEventListener('abort', abort, { once: true });
      socket.onopen = () => {
        if (signal.aborted) { abort(); return; }
        if (socket.protocol !== 'sidiora.v2') { detach(); socket.close(); reject(new StreamTransportError('UNSUPPORTED_VERSION')); return; }
        clearTimeout(timer); opened = true;
        resolve({ send: frame => { if (signal.aborted || socket.readyState !== WebSocket.OPEN) throw new StreamTransportError('TRANSPORT_UNAVAILABLE'); socket.send(JSON.stringify(decodeV2Contract('Subscribe', frame))); startPing(); }, close: () => { detach(); socket.close(); } });
      };
      socket.onmessage = event => { if (!signal.aborted) { try { const frame: unknown = JSON.parse(event.data); receive(frame); } catch { socket.close(1007, 'Invalid stream envelope'); } } };
      socket.onerror = () => { if (!opened) { detach(); socket.close(); reject(new StreamTransportError('HANDSHAKE_FAILED')); } };
      socket.onclose = () => { detach(); if (!opened) reject(new StreamTransportError('HANDSHAKE_FAILED')); else if (!signal.aborted) disconnected(); };
    }),
  };
}

export async function streamTopicFilterDigest(topics: string[]): Promise<string> {
  decodeV2Contract('Subscribe', { op: 'subscribe', subscriptionId: 'topic-digest', topics, afterCursor: null });
  const bytes = new TextEncoder().encode(JSON.stringify([...topics].sort()));
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(digest), byte => byte.toString(16).padStart(2, '0')).join('');
}
