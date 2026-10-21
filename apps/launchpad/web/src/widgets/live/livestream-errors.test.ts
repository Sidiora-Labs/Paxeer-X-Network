// Error envelopes recorded byte-for-byte from the real Go writers
// (media/livestream/internal/streams and media/gateway/internal/proxy
// upstreamUnavailable) are replayed by a real HTTP listener; the client under
// test is the production livestream-api module and viewer heartbeat.
import { createServer, type IncomingMessage, type ServerResponse } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeEach, describe, expect, it } from 'vitest';
import fixture from '@/core/test-fixtures/livestream-errors/envelopes.json';
import { plainCopy, userFacingMessage } from '@/core/user-facing-error';

interface Recorded { name: string; status: number; headers: Record<string, string>; body: string }
const rows = new Map<string, Recorded>([...fixture.livestream, ...fixture.gateway].map(row => [row.name, row as Recorded]));
const recorded = (name: string): Recorded => { const row = rows.get(name); if (!row) throw new Error(`missing fixture ${name}`); return row; };

type Route = (request: IncomingMessage, body: string) => Recorded | { status: number; json: unknown };
let route: Route = () => recorded('gateway_dead_upstream');
const seen: { method: string; url: string; body: string }[] = [];
const server = createServer((request: IncomingMessage, response: ServerResponse) => {
  let body = '';
  request.on('data', (chunk: Buffer) => { body += chunk.toString('utf8'); });
  request.on('end', () => {
    seen.push({ method: request.method ?? 'GET', url: request.url ?? '/', body });
    const answer = route(request, body);
    if ('json' in answer) { response.writeHead(answer.status, { 'Content-Type': 'application/json' }); response.end(JSON.stringify(answer.json)); return; }
    response.writeHead(answer.status, answer.headers);
    response.end(answer.body);
  });
});
await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
process.env.NEXT_PUBLIC_LIVESTREAM_API_URL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
const api = await import('@/core/clients/livestream-api');
const heartbeat = await import('./viewer-heartbeat');
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));
beforeEach(() => { seen.length = 0; route = () => recorded('gateway_dead_upstream'); });

const streamId = '11111111-1111-4111-8111-111111111111';
const sessionA = '22222222-2222-4222-8222-222222222222';
const sessionB = '33333333-3333-4333-8333-333333333333';
const later = () => new Date(Date.now() + 120_000).toISOString().replace(/\.\d{3}Z$/, 'Z');

async function failureFor(name: string) {
  route = () => recorded(name);
  const error = await api.getStream(streamId).catch((e: unknown) => e);
  expect(error).toBeInstanceOf(api.LivestreamApiError);
  return error as InstanceType<typeof api.LivestreamApiError>;
}

describe('livestream error envelopes (recorded from the Go writers)', () => {
  it('carries the top-level code, retry fields and plain copy for each recorded envelope', async () => {
    const table: [string, number, string | null, boolean | null, number | null, string][] = [
      ['broadcast_not_provisioned', 409, 'BROADCAST_NOT_PROVISIONED', false, null, plainCopy.broadcastNotProvisioned],
      ['live_document_not_provisioned', 409, 'BROADCAST_NOT_PROVISIONED', false, null, plainCopy.broadcastNotProvisioned],
      ['heartbeat_state_conflict', 409, 'STATE_CONFLICT', false, null, plainCopy.stateConflict],
      ['heartbeat_auth_expired', 401, 'AUTH_EXPIRED', false, null, plainCopy.sessionExpired],
      ['field_invalid', 422, 'FIELD_INVALID', false, null, plainCopy.invalid],
      ['revision_conflict', 409, 'REVISION_CONFLICT', false, null, 'This stream just changed, refresh and try again'],
      ['rate_limited', 429, 'RATE_LIMITED', true, 30000, 'Too many tries, wait a moment and try again'],
      ['connected_heartbeat_viewer_token', 401, 'AUTH_EXPIRED', false, null, plainCopy.sessionExpired],
      ['connected_heartbeat_state', 409, 'STATE_CONFLICT', false, null, plainCopy.stateConflict],
      ['connected_heartbeat_rate_limited', 429, 'RATE_LIMITED', true, 15000, 'Too many tries, wait a moment and try again'],
      ['connected_request_media_type', 415, 'FIELD_INVALID', false, null, plainCopy.invalid],
      ['connected_request_too_large', 413, 'FIELD_INVALID', false, null, plainCopy.invalid],
      ['connected_request_invalid_json', 400, 'FIELD_INVALID', false, null, plainCopy.invalid],
      ['gateway_dependency_unavailable', 503, 'DEPENDENCY_UNAVAILABLE', true, null, 'Live is not loading right now, try again'],
      ['gateway_dependency_unavailable_retry_after', 503, 'DEPENDENCY_UNAVAILABLE', true, 1000, 'Live is not loading right now, try again'],
      ['gateway_dead_upstream', 503, 'upstream_unavailable', null, null, plainCopy.upstreamUnavailable],
    ];
    for (const [name, status, code, retryable, retryAfterMs, message] of table) {
      const error = await failureFor(name);
      expect({ name, status: error.status, code: error.code, retryable: error.retryable, retryAfterMs: error.retryAfterMs, message: error.message }).toEqual({ name, status, code, retryable, retryAfterMs, message });
    }
  });

  it('only REVISION_CONFLICT reads as "the stream changed"; other 409s do not', async () => {
    expect((await failureFor('revision_conflict')).message).toBe('This stream just changed, refresh and try again');
    for (const name of ['broadcast_not_provisioned', 'heartbeat_state_conflict', 'connected_heartbeat_state']) expect((await failureFor(name)).message).not.toBe('This stream just changed, refresh and try again');
  });

  it('maps every recorded code to plain wording through userFacingMessage', async () => {
    expect(userFacingMessage(await failureFor('broadcast_not_provisioned'))).toBe(plainCopy.broadcastNotProvisioned);
    expect(userFacingMessage(await failureFor('heartbeat_state_conflict'))).toBe(plainCopy.stateConflict);
    expect(userFacingMessage(await failureFor('connected_heartbeat_state'))).toBe(plainCopy.stateConflict);
    expect(userFacingMessage(await failureFor('connected_heartbeat_viewer_token'))).toBe(plainCopy.sessionExpired);
    expect(userFacingMessage(await failureFor('connected_heartbeat_rate_limited'))).toBe(plainCopy.rateLimited);
    for (const name of ['connected_request_media_type', 'connected_request_too_large', 'connected_request_invalid_json']) expect(userFacingMessage(await failureFor(name))).toBe(plainCopy.invalid);
    expect(userFacingMessage(await failureFor('heartbeat_auth_expired'))).toBe(plainCopy.sessionExpired);
    expect(userFacingMessage(await failureFor('heartbeat_auth_expired'))).not.toBe(plainCopy.signIn);
    expect(userFacingMessage(await failureFor('gateway_dead_upstream'))).toBe(plainCopy.upstreamUnavailable);
    expect(userFacingMessage(await failureFor('revision_conflict'))).toBe(plainCopy.conflict);
    expect(userFacingMessage(await failureFor('field_invalid'))).toBe(plainCopy.invalid);
    expect(userFacingMessage(await failureFor('gateway_dependency_unavailable_retry_after'))).toBe(plainCopy.notAvailable);
  });

  it('a lifecycle write on an unprovisioned runtime surfaces BROADCAST_NOT_PROVISIONED, not a retry', async () => {
    route = () => recorded('broadcast_not_provisioned');
    const signing = { wallet: `0x${'a'.repeat(40)}`, environment: 'test', signMessage: async () => `0x${'1'.repeat(130)}`, assertCurrent: () => undefined };
    const error = await api.requestStream(`0x${'b'.repeat(40)}`, 'Title', 'idem-1', 'token', signing).catch((e: unknown) => e) as InstanceType<typeof api.LivestreamApiError>;
    expect(error).toBeInstanceOf(api.LivestreamApiError);
    expect([error.status, error.code, error.retryable, error.message]).toEqual([409, 'BROADCAST_NOT_PROVISIONED', false, plainCopy.broadcastNotProvisioned]);
  });
});

describe('viewer heartbeat recovery', () => {
  const heartbeatRoute = (expired: Recorded): Route => (request, body) => {
    if (request.url === `/v2/streams/${streamId}/viewer-session`) return { status: 200, json: { streamId, sessionId: sessionB, token: 'token-b', expiresAt: later() } };
    if (request.url === `/v2/streams/${streamId}/heartbeat`) {
      const sent = JSON.parse(body) as { sessionId: string; token: string };
      return sent.token === 'token-b' ? { status: 200, json: { viewerCount: 1, viewerQuality: 'verified', estimated: true } } : expired;
    }
    return recorded('field_invalid');
  };

  for (const name of ['heartbeat_auth_expired', 'connected_heartbeat_viewer_token']) {
    it(`opens a new viewer session on ${name} instead of asking the viewer to sign in`, async () => {
      route = heartbeatRoute(recorded(name));
      const outcome = await heartbeat.heartbeatOrRenew({ streamId, sessionId: sessionA, token: 'token-a', expiresAt: later() });
      expect(outcome.renewed).toBe(true);
      expect(outcome.session).toMatchObject({ streamId, sessionId: sessionB, token: 'token-b' });
      expect(seen.map(r => `${r.method} ${r.url}`)).toEqual([`POST /v2/streams/${streamId}/heartbeat`, `POST /v2/streams/${streamId}/viewer-session`, `POST /v2/streams/${streamId}/heartbeat`]);
      expect(JSON.parse(seen[2].body)).toEqual({ sessionId: sessionB, token: 'token-b' });
    });
  }

  for (const name of ['heartbeat_state_conflict', 'connected_heartbeat_state']) {
    it(`drops the session without renewing on ${name} (stream no longer LIVE)`, async () => {
      route = () => recorded(name);
      const outcome = await heartbeat.heartbeatOrRenew({ streamId, sessionId: sessionA, token: 'token-a', expiresAt: later() });
      expect(outcome).toEqual({ session: undefined, renewed: false });
      expect(seen).toHaveLength(1);
    });
  }

  it('keeps the session and reports the 15000 ms hold-off on connected_heartbeat_rate_limited', async () => {
    route = () => recorded('connected_heartbeat_rate_limited');
    const current = { streamId, sessionId: sessionA, token: 'token-a', expiresAt: later() };
    expect(await heartbeat.heartbeatOrRenew(current)).toEqual({ session: current, renewed: false, retryAfterMs: 15000 });
    expect(seen.map(r => `${r.method} ${r.url}`)).toEqual([`POST /v2/streams/${streamId}/heartbeat`]);
  });

  it('sends no heartbeat inside the 15000 ms window a rate-limited heartbeat asked for', async () => {
    let clock = 1_000_000;
    const pending: { run: () => void; ms: number; at: number }[] = [];
    const timers = { set: (run: () => void, ms: number) => { const entry = { run, ms, at: clock }; pending.push(entry); return entry; }, clear: (handle: unknown) => { const i = pending.indexOf(handle as (typeof pending)[number]); if (i >= 0) pending.splice(i, 1); } };
    const settle = async () => { for (let i = 0; i < 20 && pending.length === 0; i += 1) await new Promise(resolve => setTimeout(resolve, 5)); };
    let current = { streamId, sessionId: sessionA, token: 'token-a', expiresAt: later() };
    let limited = true;
    route = request => {
      if (request.url !== `/v2/streams/${streamId}/heartbeat`) return recorded('field_invalid');
      return limited ? recorded('connected_heartbeat_rate_limited') : { status: 200, json: { viewerCount: 1, viewerQuality: 'verified', estimated: true } };
    };
    const beat = async () => { const outcome = await heartbeat.heartbeatOrRenew(current); if (outcome.session) current = outcome.session; return outcome.retryAfterMs; };
    const loop = heartbeat.startHeartbeatLoop(beat, 30000, timers, () => clock);
    await settle();
    expect(seen).toHaveLength(1);
    expect(pending.map(p => p.ms)).toEqual([15000]);
    // A "playing" kick and the clock just short of the window both send nothing.
    clock += 14_999;
    loop.kick();
    await new Promise(resolve => setTimeout(resolve, 20));
    expect(seen).toHaveLength(1);
    // An early timer fire inside the window only re-arms for the remainder.
    pending.shift()!.run();
    await settle();
    expect(seen).toHaveLength(1);
    expect(pending.map(p => p.ms)).toEqual([1]);
    clock += 1;
    limited = false;
    pending.shift()!.run();
    await settle();
    expect(seen).toHaveLength(2);
    expect(JSON.parse(seen[1].body)).toEqual({ sessionId: sessionA, token: 'token-a' });
    expect(pending.map(p => p.ms)).toEqual([30000]);
    loop.stop();
    expect(pending).toHaveLength(0);
  });

  for (const name of ['connected_request_media_type', 'connected_request_too_large', 'connected_request_invalid_json']) {
    it(`rethrows ${name} as FIELD_INVALID without renewing or backing off`, async () => {
      route = () => recorded(name);
      const current = { streamId, sessionId: sessionA, token: 'token-a', expiresAt: later() };
      await expect(heartbeat.heartbeatOrRenew(current)).rejects.toMatchObject({ status: recorded(name).status, code: 'FIELD_INVALID', retryable: false });
      expect(seen).toHaveLength(1);
    });
  }

  it('keeps the session on success and rethrows other failures', async () => {
    route = heartbeatRoute(recorded('heartbeat_auth_expired'));
    const current = { streamId, sessionId: sessionB, token: 'token-b', expiresAt: later() };
    expect(await heartbeat.heartbeatOrRenew(current)).toEqual({ session: current, renewed: false });
    route = () => recorded('gateway_dead_upstream');
    await expect(heartbeat.heartbeatOrRenew(current)).rejects.toMatchObject({ status: 503, code: 'upstream_unavailable' });
  });
});
