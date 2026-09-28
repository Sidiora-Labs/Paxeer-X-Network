import crypto from 'node:crypto';
import fs from 'node:fs';
import http from 'node:http';
import { RemoteBrowserSession } from './session.mjs';

const host = process.env.BROWSER_PLANE_HOST ?? '127.0.0.1';
const port = Number(process.env.BROWSER_PLANE_PORT ?? 3101);
const streamEnabled = process.env.BROWSER_STREAM_ENABLED === '1';
const maxSessions = Math.min(
  32,
  Math.max(1, Number(process.env.BROWSER_PLANE_MAX_SESSIONS ?? 4)),
);
const internalKey = (() => {
  if (process.env.BROWSER_PLANE_INTERNAL_KEY) return process.env.BROWSER_PLANE_INTERNAL_KEY;
  if (process.env.BROWSER_PLANE_INTERNAL_KEY_FILE) {
    return fs.readFileSync(process.env.BROWSER_PLANE_INTERNAL_KEY_FILE, 'utf8').trim();
  }
  throw new Error('BROWSER_PLANE_INTERNAL_KEY or BROWSER_PLANE_INTERNAL_KEY_FILE is required.');
})();

if (internalKey.length < 32) {
  throw new Error('The browser-plane internal key must contain at least 32 characters.');
}

const sessions = new Map();
let creatingSessions = 0;

function safeEqual(left, right) {
  if (typeof left !== 'string' || typeof right !== 'string') return false;
  const a = Buffer.from(left);
  const b = Buffer.from(right);
  return a.length === b.length && crypto.timingSafeEqual(a, b);
}

function streamTokenEndpoint() {
  const configured =
    process.env.BROWSER_STREAM_ORIGIN ??
    'http://127.0.0.1:8081/api/browser-stream/';
  const origin = new URL(configured);
  if (
    origin.protocol !== 'http:' ||
    !['127.0.0.1', 'localhost'].includes(origin.hostname) ||
    origin.username ||
    origin.password ||
    origin.search ||
    origin.hash
  ) {
    throw new Error('The browser stream origin is invalid.');
  }
  return new URL('api/tokens', origin);
}

async function replaceStreamTokens(tokens) {
  if (!streamEnabled) return;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 5_000);
  try {
    const response = await fetch(streamTokenEndpoint(), {
      method: 'POST',
      headers: {
        Authorization: `Bearer ${internalKey}`,
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(tokens),
      signal: controller.signal,
    });
    if (!response.ok) {
      throw new Error(`The browser stream rejected token provisioning (${response.status}).`);
    }
  } finally {
    clearTimeout(timer);
  }
}

function setCommonHeaders(response) {
  response.setHeader('Cache-Control', 'no-store');
  response.setHeader('X-Content-Type-Options', 'nosniff');
  response.setHeader('Referrer-Policy', 'no-referrer');
}

function json(response, status, value, extraHeaders = {}) {
  const encoded = Buffer.from(JSON.stringify(value));
  response.writeHead(status, {
    'Content-Type': 'application/json; charset=utf-8',
    'Content-Length': String(encoded.length),
    ...extraHeaders,
  });
  response.end(encoded);
}

async function readJson(request, maxBytes = 128 * 1_024) {
  const contentType = request.headers['content-type']?.split(';', 1)[0]?.trim();
  if (contentType !== 'application/json') throw new HttpError(415, 'Expected application/json.');
  const chunks = [];
  let size = 0;
  for await (const chunk of request) {
    size += chunk.length;
    if (size > maxBytes) throw new HttpError(413, 'Request body is too large.');
    chunks.push(chunk);
  }
  try {
    const parsed = JSON.parse(Buffer.concat(chunks).toString('utf8'));
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('object required');
    }
    return parsed;
  } catch {
    throw new HttpError(400, 'Request body is not valid JSON.');
  }
}

class HttpError extends Error {
  constructor(status, message) {
    super(message);
    this.status = status;
  }
}

function requireInternalAuth(request) {
  if (!safeEqual(request.headers['x-browser-plane-key'], internalKey)) {
    throw new HttpError(401, 'Browser-plane authentication failed.');
  }
}

function bearerToken(request) {
  const header = request.headers.authorization;
  return typeof header === 'string' && header.startsWith('Bearer ')
    ? header.slice('Bearer '.length)
    : '';
}

function requireSession(request, sessionId) {
  const session = sessions.get(sessionId);
  if (!session || session.closed) {
    sessions.delete(sessionId);
    throw new HttpError(404, 'Browser session not found.');
  }
  if (!session.authorize(bearerToken(request))) {
    throw new HttpError(401, 'Browser session authentication failed.');
  }
  session.touch();
  return session;
}

function publicError(caught) {
  if (caught instanceof HttpError) return caught;
  const message =
    caught instanceof Error && /^(URL|Enter|Only HTTPS|Private network|The website|Browser|No active|Invalid|Unsupported|The last)/.test(caught.message)
      ? caught.message
      : 'The remote browser operation failed.';
  return new HttpError(500, message);
}

async function handle(request, response) {
  setCommonHeaders(response);
  requireInternalAuth(request);
  const url = new URL(request.url ?? '/', `http://${request.headers.host ?? 'localhost'}`);

  if (request.method === 'GET' && url.pathname === '/health') {
    json(response, 200, {
      status: 'ok',
      activeSessions: [...sessions.values()].filter(session => !session.closed).length,
      maxSessions,
    });
    return;
  }

  if (request.method === 'POST' && url.pathname === '/v1/sessions') {
    const activeCount = [...sessions.values()].filter(session => !session.closed).length;
    if (activeCount + creatingSessions >= maxSessions) {
      throw new HttpError(503, 'Remote browser capacity is currently full.');
    }
    const body = await readJson(request);
    creatingSessions += 1;
    let session;
    try {
      session = await RemoteBrowserSession.create(body);
      sessions.set(session.id, session);
      session.onClose(async () => {
        sessions.delete(session.id);
        await replaceStreamTokens({});
      });
      await replaceStreamTokens({
        [session.token]: {
          role: 'controller',
          slot: null,
          mk_control: true,
        },
      });
      json(response, 201, {
        ...session.state(),
        token: session.token,
        streamPath: streamEnabled ? '/api/browser-stream/' : null,
      });
    } catch (caught) {
      if (session && !session.closed) {
        await session.close('create-failed').catch(() => undefined);
      }
      throw caught;
    } finally {
      creatingSessions -= 1;
    }
    return;
  }

  const sessionMatch = url.pathname.match(
    /^\/v1\/sessions\/([0-9a-f-]{36})(?:\/(.*))?$/,
  );
  if (!sessionMatch) throw new HttpError(404, 'Browser-plane endpoint not found.');
  const [, sessionId, remainder = ''] = sessionMatch;
  const session = requireSession(request, sessionId);

  if (request.method === 'DELETE' && remainder === '') {
    await session.close('client-close');
    response.writeHead(204);
    response.end();
    return;
  }

  if (request.method === 'GET' && remainder === 'state') {
    json(response, 200, session.state());
    return;
  }

  if (request.method === 'GET' && remainder === 'events') {
    const rawCursor = Number(url.searchParams.get('cursor') ?? 0);
    const cursor = Number.isSafeInteger(rawCursor) && rawCursor >= 0 ? rawCursor : 0;
    json(response, 200, await session.eventsAfter(cursor));
    return;
  }

  if (request.method === 'GET' && remainder === 'frame') {
    const rawVersion = Number(url.searchParams.get('after') ?? 0);
    const version = Number.isSafeInteger(rawVersion) && rawVersion >= 0 ? rawVersion : 0;
    const frame = await session.frameAfter(version);
    if (!frame) {
      response.writeHead(204);
      response.end();
      return;
    }
    response.writeHead(200, {
      'Content-Type': 'image/jpeg',
      'Content-Length': String(frame.buffer.length),
      'X-Frame-Version': String(frame.version),
    });
    response.end(frame.buffer);
    return;
  }

  if (request.method === 'POST' && remainder === 'navigation') {
    const body = await readJson(request);
    json(response, 200, await session.navigate(body.action, body.url));
    return;
  }

  if (request.method === 'POST' && remainder === 'input') {
    json(response, 200, await session.dispatchInput(await readJson(request, 8 * 1_024)));
    return;
  }

  const rpcMatch = remainder.match(/^rpc\/([0-9a-f-]{36})$/);
  if (request.method === 'POST' && rpcMatch) {
    session.resolveRpc(rpcMatch[1], await readJson(request));
    response.writeHead(204);
    response.end();
    return;
  }

  if (request.method === 'POST' && remainder === 'provider-event') {
    const body = await readJson(request);
    await session.emitProviderEvent(body.event, body.payload);
    response.writeHead(204);
    response.end();
    return;
  }

  if (request.method === 'POST' && remainder === 'tabs') {
    const body = await readJson(request);
    json(response, 201, await session.createTab(body.url));
    return;
  }

  const activateMatch = remainder.match(/^tabs\/([0-9a-f-]{36})\/activate$/);
  if (request.method === 'POST' && activateMatch) {
    await session.activateTab(activateMatch[1]);
    json(response, 200, session.state());
    return;
  }

  const closeTabMatch = remainder.match(/^tabs\/([0-9a-f-]{36})$/);
  if (request.method === 'DELETE' && closeTabMatch) {
    json(response, 200, await session.closeTab(closeTabMatch[1]));
    return;
  }

  throw new HttpError(404, 'Browser-plane endpoint not found.');
}

const server = http.createServer((request, response) => {
  void handle(request, response).catch(caught => {
    if (response.headersSent) {
      response.destroy();
      return;
    }
    const error = publicError(caught);
    json(response, error.status, { error: error.message });
  });
});

server.requestTimeout = 35_000;
server.headersTimeout = 10_000;
server.keepAliveTimeout = 5_000;
server.listen(port, host, () => {
  process.stdout.write(`PaxPort browser plane listening on ${host}:${port}\n`);
});

async function shutdown(signal) {
  server.close();
  await Promise.all([...sessions.values()].map(session => session.close(signal)));
  process.exit(0);
}

process.on('SIGINT', () => void shutdown('SIGINT'));
process.on('SIGTERM', () => void shutdown('SIGTERM'));
