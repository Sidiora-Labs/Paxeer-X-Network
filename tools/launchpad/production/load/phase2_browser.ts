export interface BrowserDelivery {
  receiver_id: string;
  event_id: string;
  topic: string;
  sequence: string;
  revision: string;
  received_utc_ns: string;
}

export interface BrowserReceiverConfig {
  applicationOrigin: string;
  streamOrigin: string;
  topics: string[];
  receiverId: string;
  bearer?: string;
  durationMs: number;
  signal: AbortSignal;
}

export async function observeBrowserDelivery(config: BrowserReceiverConfig, emit: (sample: BrowserDelivery) => Promise<void>): Promise<{ frames: number; checkpoints: number; cursor: string }> {
  if (location.origin !== config.applicationOrigin || !Number.isFinite(config.durationMs) || config.durationMs < 600000 || config.durationMs > 1800000 || config.topics.length < 1 || config.topics.length > 20 || new Set(config.topics).size !== config.topics.length) throw new Error('Actual admitted browser origin, scope and ten-minute interval required');
  const origin = new URL(config.streamOrigin);
  if (origin.protocol !== 'https:' || origin.origin !== config.streamOrigin) throw new Error('Exact HTTPS stream origin required');
  const headers: Record<string, string> = { Accept: 'application/json' };
  if (config.bearer) headers.Authorization = `Bearer ${config.bearer}`;
  const topics = [...config.topics].sort();
  const snapshotResponse = await fetch(new URL(`/v2/snapshots?topics=${encodeURIComponent(topics.join(','))}`, origin), { headers, signal: config.signal, credentials: 'omit', redirect: 'error' });
  if (!snapshotResponse.ok) throw new Error('Actual snapshot unavailable');
  const snapshot = (await snapshotResponse.json()).data.snapshot;
  const sequences = new Map<string, bigint>();
  const revisions = new Map<string, bigint>();
  for (const row of snapshot.topics) { sequences.set(row.topic, decimal(row.sequence)); revisions.set(row.topic, decimal(row.revision)); }
  if (sequences.size !== topics.length || topics.some(topic => !sequences.has(topic)) || typeof snapshot.resumeCursor !== 'string') throw new Error('Snapshot scope mismatch');
  const protocols = ['sidiora.v2'];
  if (config.bearer) {
    const hash = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(JSON.stringify(topics)));
    const filterDigest = [...new Uint8Array(hash)].map(byte => byte.toString(16).padStart(2, '0')).join('');
    const response = await fetch(new URL('/v2/stream/tickets', config.applicationOrigin), { method: 'POST', headers: { ...headers, 'Content-Type': 'application/json' }, body: JSON.stringify({ topics, schemaMajor: 2, filterDigest, origin: config.applicationOrigin, endpoint: '/v2/stream' }), credentials: 'omit', redirect: 'error', signal: config.signal });
    if (!response.ok) throw new Error('Actual scoped ticket unavailable');
    const ticket = (await response.json()).data;
    if (!/^[0-9a-f]{64}$/.test(ticket.ticket) || ticket.subprotocol !== 'sidiora.v2' || Date.parse(ticket.expiresAt) <= Date.now()) throw new Error('Invalid scoped ticket');
    protocols.push(`ticket.${ticket.ticket}`);
  }
  let cursor: string = snapshot.resumeCursor;
  let frames = 0;
  let checkpoints = 0;
  const address = new URL('/v2/stream', origin);
  address.protocol = 'wss:';
  return await new Promise((resolve, reject) => {
    const socket = new WebSocket(address, protocols);
    let chain = Promise.resolve();
    let finished = false;
    let timer: ReturnType<typeof setTimeout>;
    const clean = () => { clearTimeout(timer); config.signal.removeEventListener('abort', abort); socket.onmessage = null; socket.onerror = null; socket.onclose = null; socket.close(); };
    const fail = (error: unknown) => { if (!finished) { finished = true; clean(); reject(error); } };
    const abort = () => fail(config.signal.reason);
    config.signal.addEventListener('abort', abort, { once: true });
    if (config.signal.aborted) { abort(); return; }
    const handshakeTimer = setTimeout(() => fail(new Error('Browser handshake deadline')), 5000);
    socket.onopen = () => {
      clearTimeout(handshakeTimer);
      if (socket.protocol !== 'sidiora.v2') { fail(new Error('Negotiated stream version differs')); return; }
      socket.send(JSON.stringify({ op: 'subscribe', subscriptionId: config.receiverId, topics, afterCursor: cursor }));
      timer = setTimeout(() => { chain.then(() => { if (finished) return; finished = true; clean(); if (frames < 1 || checkpoints < 1) reject(new Error('Actual browser frames and checkpoints required')); else resolve({ frames, checkpoints, cursor }); }, fail); }, config.durationMs);
    };
    socket.onmessage = event => {
      const received = BigInt(Math.round((performance.timeOrigin + performance.now()) * 1000000)).toString();
      chain = chain.then(async () => {
        if (typeof event.data !== 'string' || event.data.length > 262144) throw new Error('Bounded text browser frame required');
        const value = JSON.parse(event.data);
        if (value.op) {
          if (['error', 'resnapshot_required', 'integrity_incident', 'subscription_closed'].includes(value.op)) throw new Error('Explicit stream recovery required');
          if (value.op === 'checkpoint') { if (typeof value.cursor !== 'string' || value.cursor.length > 2048) throw new Error('Invalid browser checkpoint'); cursor = value.cursor; checkpoints++; }
          return;
        }
        if (!sequences.has(value.topic)) throw new Error('Browser event outside scope');
        const sequence = decimal(value.sequence), revision = decimal(value.revision);
        if (revision < revisions.get(value.topic)! || sequence <= sequences.get(value.topic)!) throw new Error('Browser source regression');
        if (value.coalesced) {
          if (!value.topic.startsWith('market.price.') || decimal(value.sourceSequenceFirst) !== sequences.get(value.topic)! + 1n || decimal(value.sourceSequenceLast) !== sequence) throw new Error('Invalid coalesced browser source range');
        } else if (sequence !== sequences.get(value.topic)! + 1n || value.sourceSequenceFirst !== null || value.sourceSequenceLast !== null) throw new Error('Browser durable sequence gap');
        if (typeof value.eventId !== 'string') throw new Error('Browser immutable event identity required');
        sequences.set(value.topic, sequence); revisions.set(value.topic, revision); frames++;
        await emit({ receiver_id: config.receiverId, event_id: value.eventId, topic: value.topic, sequence: value.sequence, revision: value.revision, received_utc_ns: received });
      }).catch(fail);
    };
    socket.onerror = () => fail(new Error('Actual browser stream failed'));
    socket.onclose = () => fail(new Error('Browser stream closed before full interval'));
  });
}

function decimal(value: unknown): bigint {
  if (typeof value !== 'string' || !/^(0|[1-9][0-9]*)$/.test(value) || value.length > 78) throw new Error('Exact decimal sequence/revision required');
  return BigInt(value);
}
