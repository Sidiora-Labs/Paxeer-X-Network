/**
 * Social REST + chat socket against a local fixture of the PRODUCTION gateway
 * surface (media/gateway, NODE_ENV=production):
 *
 * - POST /v2/stream/tickets (auth/identity_transport.go "/v2/stream/tickets"):
 *   strict body {schemaMajor, topics, filterDigest, origin, endpoint}; chat
 *   topics select the chat issuer (auth/identity_chat.go chatBinding), which
 *   applies shared/auth contract.go ValidateChatStreamBinding; the answer is the
 *   identity envelope {requestId, data: {ticket, expiresAt, subprotocol}, ...}
 *   with a 32-byte lowercase hex ticket (identity_models.go randomSecret).
 * - GET /v2/stream (proxy/connected_ws.go ServeHTTP): only `topics` in the
 *   query, subprotocols exactly `sidiora.v2` + `ticket.<64 hex>`; the hub then
 *   speaks the media/social protocol v2 frames (fanout/hub.go ServeWS
 *   session_accepted, fanout/message.go join_pool/joined_pool, pool_message ->
 *   message_persisted, durable.go DurableMessage events).
 * - /v2/chat/history (fanout/http.go HistoryPage), /v2/social/* (devsocial
 *   interactionData envelope), /v2/safety/reports, all session-bound
 *   (proxy/connected.go SocialConnectedRoutes Authenticate) and reached
 *   cross-site with a bearer, never a cookie.
 *
 * Not reproduced here: the real gateway also authenticates the socket upgrade
 * itself (AuthenticateRequest: bearer or __Host-kindle_access cookie). A
 * cross-site browser socket carries neither, so against the live gateway the
 * upgrade answers 401 until the gateway admits the ticket alone.
 */
import { createHash, randomBytes } from 'node:crypto';
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { createRequire } from 'node:module';
import { afterAll, afterEach, describe, expect, it } from 'vitest';
import type { Duplex } from 'node:stream';

interface BrowserDom { window: Window & typeof globalThis }
type JsdomModule = { JSDOM: new (html: string, options: { url: string }) => BrowserDom };
const { JSDOM } = createRequire(import.meta.url)('jsdom') as JsdomModule;
// The `ws` server (no bundled typings): the surface this fixture uses.
interface ServerSocket { send(data: string): void; on(event: 'message', listener: (raw: Buffer) => void): void }
interface WebSocketServerInstance { handleUpgrade(request: IncomingMessage, socket: Duplex, head: Buffer, done: (ws: ServerSocket) => void): void; close(): void }
type WsModule = { WebSocketServer: new (options: { noServer: true; handleProtocols: (protocols: Set<string>) => string | false }) => WebSocketServerInstance };
const { WebSocketServer } = createRequire(import.meta.url)('ws') as WsModule;
const pageOrigin = 'https://web-production-d956e.up.railway.app';
const dom = new JSDOM('<!DOCTYPE html><html><body></body></html>', { url: pageOrigin });
const browserGlobals = globalThis as unknown as Record<string, unknown>;
browserGlobals.window = dom.window;
browserGlobals.document = dom.window.document;

const wallet = '0x' + '1d'.repeat(20);
const pool = '0xce20da50f92d21d8bee497910c782e9aa25b6263';
const token = 'gateway-access-token-' + 'a'.repeat(24);
const walletPattern = /^0x[0-9a-f]{40}$/;

type Recorded = { method: string; path: string; authorization?: string; cookie?: string; body: string };
const requests: Recorded[] = [];
const tickets = new Map<string, { topics: string[]; origin: string }>();

/** shared/auth contract.go StreamFilterDigest: sha256 of json.Marshal(sorted topics), lowercase hex. */
const filterDigest = (topics: string[]) => createHash('sha256').update(JSON.stringify([...topics].sort())).digest('hex');

/** shared/auth contract.go ValidateChatStreamBinding for the signed-in wallet. */
function validChatBinding(binding: Record<string, unknown>): boolean {
  const topics = binding.topics;
  if (binding.schemaMajor !== 2 || binding.endpoint !== '/v2/stream' || !Array.isArray(topics) || topics.length < 1 || topics.length > 20) return false;
  if (!topics.every(topic => typeof topic === 'string') || binding.filterDigest !== filterDigest(topics as string[])) return false;
  if (typeof binding.origin !== 'string') return false;
  const origin = new URL(binding.origin);
  if (origin.protocol !== 'https:' || origin.origin !== binding.origin) return false;
  if (new Set(topics).size !== topics.length) return false;
  return (topics as string[]).every(topic => {
    const [scope, kind, target, extra] = topic.split('.');
    if (extra !== undefined || !target || !walletPattern.test(target)) return false;
    return (scope === 'chat' && kind === 'pool') || (scope === 'private' && kind === 'chat' && target === wallet);
  });
}

const envelope = (data: unknown) => ({ requestId: '6f1d3a52-6b8e-4c43-9a5c-0d3f9e7d1a10', data, quality: 'complete', generatedAt: new Date().toISOString() });

function json(response: ServerResponse, status: number, body: unknown) {
  response.statusCode = status;
  response.setHeader('Content-Type', 'application/json');
  response.setHeader('Cache-Control', 'no-store');
  response.end(JSON.stringify(body));
}

const historyMessage = { pool_address: pool, type: 'pool_message', id: 'msg-1', conversation_id: `pool:${pool}`, sender: wallet, content: 'gm', sequence: '7', revision: '1', created_at: 1_760_000_000, deleted: false };

function route(request: IncomingMessage, response: ServerResponse, body: string) {
  const url = new URL(request.url ?? '/', 'http://fixture');
  requests.push({ method: request.method ?? 'GET', path: url.pathname + url.search, authorization: request.headers.authorization, cookie: request.headers.cookie, body });
  const authorized = request.headers.authorization === `Bearer ${token}`;
  if (request.method === 'POST' && url.pathname === '/v2/stream/tickets') {
    if (!authorized) return json(response, 401, { code: 'AUTH_EXPIRED', message: 'Session expired', retryable: false });
    let binding: Record<string, unknown>;
    try { binding = JSON.parse(body) as Record<string, unknown>; } catch { return json(response, 400, { code: 'FIELD_INVALID' }); }
    const keys = Object.keys(binding).sort().join(',');
    if (keys !== 'endpoint,filterDigest,origin,schemaMajor,topics') return json(response, 400, { code: 'FIELD_UNKNOWN' });
    const origin = request.headers.origin;
    if (origin !== undefined && origin !== binding.origin) return json(response, 403, { code: 'SCOPE_DENIED' });
    if (binding.origin !== pageOrigin || !validChatBinding(binding)) return json(response, 403, { code: 'SCOPE_DENIED' });
    const ticket = randomBytes(32).toString('hex');
    tickets.set(ticket, { topics: [...(binding.topics as string[])].sort(), origin: binding.origin as string });
    return json(response, 200, envelope({ ticket, expiresAt: new Date(Date.now() + 30_000).toISOString(), subprotocol: 'sidiora.v2' }));
  }
  if (url.pathname.startsWith('/v2/chat/') || url.pathname.startsWith('/v2/social/') || url.pathname.startsWith('/v2/safety/')) {
    if (!authorized) return json(response, 401, { code: 'AUTH_EXPIRED' });
  }
  if (request.method === 'GET' && url.pathname === '/v2/chat/history') {
    if (url.searchParams.get('conversation_id') !== `pool:${pool}`) return json(response, 404, { error: 'not_found', next_cursor: '', reset_required: false });
    return json(response, 200, { read_through_sequence: '7', reset_required: false, messages: [historyMessage], next_cursor: 'cursor-2' });
  }
  if (request.method === 'GET' && url.pathname === `/v2/social/users/${pool}/follow-stats`) {
    return json(response, 200, { ...envelope({ wallet: pool, followers: '3', following: '1', is_following: true, revision: '0' }), nextCursor: null, watermark: null, revision: null });
  }
  if (request.method === 'PUT' && url.pathname === `/v2/social/follow/${pool}`) {
    if (!request.headers['idempotency-key']) return json(response, 400, { code: 'INVALID_INPUT' });
    return json(response, 200, { ...envelope({ following: true }), nextCursor: null, watermark: null, revision: null });
  }
  if (request.method === 'POST' && url.pathname === '/v2/safety/reports') {
    const command = JSON.parse(body) as { targetType?: string; targetId?: string };
    return json(response, 201, { id: 'case-1', targetType: command.targetType, targetId: command.targetId, state: 'open', revision: 1 });
  }
  json(response, 404, { code: 'NOT_FOUND' });
}

const server = createServer((request, response) => {
  const chunks: Buffer[] = [];
  request.on('data', chunk => chunks.push(chunk as Buffer));
  request.on('end', () => route(request, response, Buffer.concat(chunks).toString('utf8')));
});

type Upgrade = { topics: string[]; protocols: string[]; authorization?: string };
const upgrades: Upgrade[] = [];
const sockets = new WebSocketServer({
  noServer: true,
  handleProtocols: (protocols: Set<string>) => (protocols.has('sidiora.v2') ? 'sidiora.v2' : false),
});
server.on('upgrade', (request, socket, head) => {
  const url = new URL(request.url ?? '/', 'http://fixture');
  const protocols = String(request.headers['sec-websocket-protocol'] ?? '').split(',').map(value => value.trim()).filter(Boolean);
  const refuse = (status: number) => { socket.end(`HTTP/1.1 ${status} Refused\r\nConnection: close\r\nContent-Length: 0\r\n\r\n`); };
  if (url.pathname !== '/v2/stream' || [...url.searchParams.keys()].some(key => key !== 'topics') || url.searchParams.getAll('topics').length !== 1) return refuse(401);
  const tickets_ = protocols.filter(value => value.startsWith('ticket.'));
  if (protocols.filter(value => value === 'sidiora.v2').length !== 1 || tickets_.length !== 1 || protocols.length !== 2) return refuse(400);
  const ticket = tickets_[0]!.slice('ticket.'.length);
  const topics = (url.searchParams.get('topics') ?? '').split(',');
  const issued = tickets.get(ticket);
  tickets.delete(ticket);
  if (!/^[0-9a-f]{64}$/.test(ticket) || !issued || issued.topics.join(',') !== [...topics].sort().join(',')) return refuse(401);
  upgrades.push({ topics, protocols, authorization: request.headers.authorization });
  sockets.handleUpgrade(request, socket, head, (ws: ServerSocket) => serveHub(ws, topics));
});

/** media/social fanout hub, protocol v2 (hub.go ServeWS, message.go handleMessage). */
function serveHub(ws: ServerSocket, topics: string[]) {
  ws.send(JSON.stringify({ type: 'session_accepted', protocol_version: 2 }));
  let sequence = 7;
  ws.on('message', (raw: Buffer) => {
    const frame = JSON.parse(String(raw)) as { type?: string; pool_address?: string; content?: string; client_message_id?: string };
    if (frame.type === 'join_pool') {
      const target = String(frame.pool_address).toLowerCase();
      if (!topics.includes(`chat.pool.${target}`)) { ws.send(JSON.stringify({ type: 'error', message: 'subscription denied' })); return; }
      ws.send(JSON.stringify({ type: 'joined_pool', pool_address: target }));
      return;
    }
    if (frame.type === 'pool_message') {
      sequence++;
      const id = `msg-${sequence}`;
      ws.send(JSON.stringify({ type: 'message_persisted', client_message_id: frame.client_message_id, id, sequence: String(sequence), revision: '1', conversation_id: `pool:${pool}` }));
      ws.send(JSON.stringify({ pool_address: pool, type: 'pool_message', id, client_message_id: frame.client_message_id, conversation_id: `pool:${pool}`, sender: wallet, content: frame.content, sequence: String(sequence), revision: '1', created_at: 1_760_000_100, deleted: false }));
      return;
    }
    ws.send(JSON.stringify({ type: 'error', message: `Unknown message type: ${frame.type}` }));
  });
}

await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
const gatewayOrigin = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
process.env.NEXT_PUBLIC_GATEWAY_URL = gatewayOrigin;
delete process.env.NEXT_PUBLIC_SOCIAL_WRITE_PREFIX;
delete process.env.NEXT_PUBLIC_SOCIAL_WS_PATH;
delete process.env.NEXT_PUBLIC_SOCIAL_API_URL;

const config = await import('@/core/sdk-config');
const chatApi = await import('./chat-api');
const chatWs = await import('./chat-ws');
const account = await import('@/widgets/profile/account-transport');
const socialTransport = await import('@/widgets/social/social-transport');
const { SafetyClient, SafetyRequestError } = await import('@/widgets/community-safety/safety-client');

function signIn() {
  account.installAccountSession({ accessExpiresAt: new Date(Date.now() + 900_000).toISOString(), deviceLabel: 'Web browser', refreshExpiresAt: new Date(Date.now() + 86_400_000).toISOString(), revokedAt: null, scopes: ['account'], sessionId: 'sess-social', wallet }, token, account.accountEpoch());
}

function waitFor<T>(read: () => T | undefined, timeoutMs = 5_000): Promise<T> {
  const started = Date.now();
  return new Promise((resolve, reject) => {
    const poll = () => {
      const value = read();
      if (value !== undefined) { resolve(value); return; }
      if (Date.now() - started > timeoutMs) { reject(new Error('timed out')); return; }
      setTimeout(poll, 10);
    };
    poll();
  });
}

afterEach(() => { chatWs.disconnect(); account.clearAccountSession(false); socialTransport.resetFollowRoute(); });
afterAll(async () => { sockets.close(); await new Promise(resolve => server.close(resolve)); });

describe('production social endpoints', () => {
  it('addresses the gateway at root and the chat socket at the absolute gateway /v2/stream', () => {
    expect(config.socialWriteUrl('/v2/chat/history')).toBe(`${gatewayOrigin}/v2/chat/history`);
    expect(config.socialReadUrl('/v2/profiles/x/follow-stats')).toBe(`${gatewayOrigin}/v2/profiles/x/follow-stats`);
    expect(config.socialWsUrl()).toBe(`${gatewayOrigin.replace('http', 'ws')}/v2/stream`);
  });
});

describe('chat ticket and protocol v2 socket', () => {
  it('refuses to request a ticket without a session and sends nothing', async () => {
    const before = requests.length;
    await expect(chatApi.issueChatTicket([`chat.pool.${pool}`], new AbortController().signal)).rejects.toMatchObject({ status: 401 });
    expect(requests.length).toBe(before);
  });

  it('issues a topic-bound ticket with the bearer and the Go filter digest', async () => {
    signIn();
    const topics = [`private.chat.${wallet}`, `chat.pool.${pool}`];
    expect(await chatApi.streamFilterDigest(topics)).toBe(filterDigest(topics));
    const ticket = await chatApi.issueChatTicket(topics, new AbortController().signal);
    expect(ticket.ticket).toMatch(/^[0-9a-f]{64}$/);
    expect(ticket.subprotocol).toBe('sidiora.v2');
    const sent = requests.at(-1)!;
    expect(sent).toMatchObject({ method: 'POST', path: '/v2/stream/tickets', authorization: `Bearer ${token}`, cookie: undefined });
    expect(JSON.parse(sent.body)).toEqual({ schemaMajor: 2, topics, filterDigest: filterDigest(topics), origin: pageOrigin, endpoint: '/v2/stream' });
  });

  it('surfaces the gateway SCOPE_DENIED for a topic outside the session', async () => {
    signIn();
    const other = '0x' + '2e'.repeat(20);
    await expect(chatApi.issueChatTicket([`private.chat.${other}`], new AbortController().signal)).rejects.toMatchObject({ status: 403, code: 'SCOPE_DENIED' });
  });

  it('opens the socket with sidiora.v2 + ticket subprotocols, joins the pool and receives the durable echo', async () => {
    signIn();
    const statuses: string[] = [];
    const persisted: string[] = [];
    const messages: string[] = [];
    let replayed = 0;
    chatWs.addConsumer({ wallet }, {
      onStatusChange: status => statuses.push(status),
      onPersisted: ack => persisted.push(ack.clientMessageId),
      onPoolMessage: message => messages.push(`${message.sequence}:${message.content}`),
      onReplay: () => { replayed++; },
    });
    const upgradesBefore = upgrades.length;
    chatWs.joinPool(pool.toUpperCase().replace('0X', '0x'));
    await waitFor(() => (replayed > 0 ? true : undefined));
    expect(chatWs.getStatus()).toBe('connected');
    const upgrade = upgrades[upgradesBefore]!;
    expect(upgrade.topics).toEqual([`chat.pool.${pool}`]);
    expect(upgrade.protocols[0]).toBe('sidiora.v2');
    expect(upgrade.protocols[1]).toMatch(/^ticket\.[0-9a-f]{64}$/);
    expect(statuses).toEqual(expect.arrayContaining(['authenticating', 'connecting', 'connected']));
    const clientId = chatWs.sendPoolMessage(pool, 'hello production');
    await waitFor(() => (messages.length ? true : undefined));
    expect(persisted).toEqual([clientId]);
    expect(messages).toEqual(['8:hello production']);
  });

  it('reads chat history from the gateway with the bearer and decodes the durable page', async () => {
    await expect(chatApi.getPoolMessages(pool)).rejects.toMatchObject({ status: 401 });
    signIn();
    const page = await chatApi.getPoolMessages(pool, { limit: 50 });
    expect(requests.at(-1)).toMatchObject({ method: 'GET', path: `/v2/chat/history?conversation_id=pool%3A${pool}&limit=50`, authorization: `Bearer ${token}`, cookie: undefined });
    expect(page).toMatchObject({ nextCursor: 'cursor-2', readThroughSequence: '7', resetRequired: false });
    expect(page.messages[0]).toMatchObject({ id: 'msg-1', sequence: '7', content: 'gm', poolAddress: pool });
  });
});

describe('social and safety requests are session-bound bearer calls', () => {
  it('falls back from the unserved follow operation to /v2/social/follow with an idempotency key', async () => {
    signIn();
    const result = await socialTransport.setFollowing(pool, false, 'idem-1');
    expect(result).toEqual({ following: true, route: 'social-proxy' });
    const put = requests.filter(entry => entry.path === `/v2/social/follow/${pool}`).at(-1);
    expect(put).toMatchObject({ method: 'PUT', authorization: `Bearer ${token}`, cookie: undefined });
  });

  it('reads viewer-aware follow stats from /v2/social when the contract read is unserved', async () => {
    signIn();
    const stats = await socialTransport.fetchFollowStats(pool);
    expect(stats).toEqual({ wallet: pool, followers: 3, following: 1, isFollowing: true, route: 'social-proxy' });
  });

  it('reports the contract miss to a signed-out visitor instead of calling the session-only route', async () => {
    const before = requests.filter(entry => entry.path.startsWith('/v2/social/')).length;
    await expect(socialTransport.fetchFollowStats(pool)).rejects.toMatchObject({ status: 404, route: 'operation' });
    expect(requests.filter(entry => entry.path.startsWith('/v2/social/')).length).toBe(before);
  });

  it('files a safety report with the bearer and refuses locally without a session', async () => {
    const client = new SafetyClient(gatewayOrigin.replace('127.0.0.1', 'localhost'), () => account.accountSession()?.accessToken);
    await expect(client.report({ targetType: 'message', targetId: 'msg-1', category: 'spam', description: 'repeated links' })).rejects.toBeInstanceOf(SafetyRequestError);
    signIn();
    const created = await client.report({ targetType: 'message', targetId: 'msg-1', category: 'spam', description: 'repeated links' });
    expect(created).toMatchObject({ id: 'case-1', targetId: 'msg-1' });
    expect(requests.at(-1)).toMatchObject({ method: 'POST', path: '/v2/safety/reports', authorization: `Bearer ${token}`, cookie: undefined });
  });
});
