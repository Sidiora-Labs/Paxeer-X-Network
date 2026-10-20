// @vitest-environment jsdom
import React, { act } from 'react';
import { createServer } from 'node:http';
import type { AddressInfo } from 'node:net';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { createRoot } from 'react-dom/client';

beforeAll(() => { (globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true; });

const streamId = '11111111-1111-4111-8111-111111111111';
const sessionRequests: string[] = [];
// A real HTTP listener stands in for the livestream host: the first viewer-session
// request answers 503, every later one issues a session.
const server = createServer((request, response) => {
  const url = new URL(request.url ?? '/', 'http://127.0.0.1');
  if (request.method === 'POST' && url.pathname === `/v2/streams/${streamId}/viewer-session`) {
    sessionRequests.push(url.pathname);
    if (sessionRequests.length === 1) { response.writeHead(503, { 'Content-Type': 'application/json' }); response.end('{"code":"upstream_unavailable"}'); return; }
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ streamId, sessionId: '22222222-2222-4222-8222-222222222222', token: 'viewer-token', expiresAt: new Date(Date.now() + 600000).toISOString() }));
    return;
  }
  response.writeHead(404, { 'Content-Type': 'application/json' });
  response.end('{}');
});
await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve));
process.env.NEXT_PUBLIC_LIVESTREAM_API_URL = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
const api = await import('@/core/clients/livestream-api');
const { StreamPlayer } = await import('./stream-player');
afterAll(() => new Promise<void>(resolve => server.close(() => resolve())));

async function until(check: () => boolean) {
  const deadline = Date.now() + 5000;
  while (!check()) {
    if (Date.now() > deadline) throw new Error('condition not reached');
    await act(() => new Promise(resolve => setTimeout(resolve, 10)));
  }
}

describe('stream player viewer session', () => {
  it('offers a retry after the first viewer-session request fails and plays on with the unchanged stream', async () => {
    const stream = api.parseStream({ id: streamId, poolAddress: `0x${'a'.repeat(40)}`, creatorWallet: `0x${'b'.repeat(40)}`, title: 'Launch night', playbackUrl: 'https://livepeercdn.studio/hls/stream1/index.m3u8', playbackId: 'stream1', thumbnailUrl: null, isLive: true, viewerCount: 1, startedAt: 1, endedAt: null, createdAt: 1 });
    const container = document.createElement('div');
    document.body.appendChild(container);
    const root = createRoot(container);
    const player = () => container.querySelector<HTMLElement>('[data-player-state]')!;
    const retry = () => Array.from(container.querySelectorAll('button')).find(button => button.textContent === 'Try again');
    act(() => root.render(<StreamPlayer stream={stream} />));

    await until(() => player().dataset.playerState === 'unavailable');
    expect(sessionRequests).toHaveLength(1);
    expect(retry()).toBeDefined();

    act(() => retry()!.click());
    await until(() => sessionRequests.length === 2 && player().dataset.playerState !== 'unavailable' && player().dataset.playerState !== 'connecting');
    expect(player().dataset.playerState).not.toBe('unavailable');
    act(() => root.unmount());
  });
});
