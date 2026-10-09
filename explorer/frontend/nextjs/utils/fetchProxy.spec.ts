import type { RequestInit } from 'node-fetch';
import type { IncomingHttpHeaders, Server } from 'node:http';
import { createServer, IncomingMessage } from 'node:http';
import type { AddressInfo } from 'node:net';
import { Socket } from 'node:net';

import { NAMES } from 'lib/cookies';
import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';

import fetchFactory from './fetchProxy';

interface Received {
  method?: string;
  headers: IncomingHttpHeaders;
  body: string;
}

const received: Array<Received> = [];
let server: Server;
let origin: string;

const createRequest = (headers: IncomingHttpHeaders, cookies: Record<string, string> = {}) => {
  const request = Object.assign(new IncomingMessage(new Socket()), { cookies });
  request.headers = headers;
  return request;
};

describe('fetchProxy', () => {
  beforeAll(async() => {
    server = createServer((request, response) => {
      const chunks: Array<Buffer> = [];
      request.on('data', (chunk: Buffer) => chunks.push(chunk));
      request.on('end', () => {
        received.push({ method: request.method, headers: request.headers, body: Buffer.concat(chunks).toString() });
        response.writeHead(201, { 'content-type': 'application/json' });
        response.end(JSON.stringify({ ok: true }));
      });
    });
    await new Promise<void>((resolve) => {
      server.listen(0, resolve);
    });
    origin = `http://localhost:${ (server.address() as AddressInfo).port }`;
  });

  afterAll(async() => {
    await new Promise<void>((resolve) => {
      server.close(() => resolve());
    });
  });

  beforeEach(() => {
    received.length = 0;
  });

  it('returns the upstream response to the caller', async() => {
    const response = await fetchFactory(createRequest({}))(`${ origin }/api/v2/stats`);

    expect(response.status).toBe(201);
    expect(await response.json()).toEqual({ ok: true });
    expect(received[0].method).toBe('GET');
  });

  it('forwards only the cookies the explorer itself sets', async() => {
    const request = createRequest({}, {
      [NAMES.API_TOKEN]: 'api-token',
      [NAMES.REWARDS_API_TOKEN]: 'rewards-token',
      session_id: 'foreign-session',
    });

    await fetchFactory(request)(`${ origin }/api/v2/stats`);

    expect(received[0].headers.cookie).toBe(`${ NAMES.API_TOKEN }=api-token; ${ NAMES.REWARDS_API_TOKEN }=rewards-token`);
  });

  it('asks for json when the incoming request names no media types', async() => {
    await fetchFactory(createRequest({}))(`${ origin }/api/v2/stats`);

    expect(received[0].headers.accept).toBe('application/json');
    expect(received[0].headers['content-type']).toBe('application/json');
  });

  it('keeps the media types the incoming request names', async() => {
    await fetchFactory(createRequest({ accept: 'text/csv', 'content-type': 'text/plain' }))(`${ origin }/api/v2/stats`);

    expect(received[0].headers.accept).toBe('text/csv');
    expect(received[0].headers['content-type']).toBe('text/plain');
  });

  it('passes the allowed headers through and drops every other one', async() => {
    const request = createRequest({
      'x-csrf-token': 'csrf',
      'recaptcha-v2-response': 'recaptcha',
      'user-agent': 'explorer-spec',
      authorization: 'Bearer token',
      'show-scam-tokens': 'true',
      'api-v2-temp-token': 'temp-token',
      'updated-gas-oracle': 'true',
      'x-endpoint': 'upstream-override',
      'x-forwarded-for': 'forwarded',
    });

    await fetchFactory(request)(`${ origin }/api/v2/stats`);

    const { headers } = received[0];

    expect(headers['x-csrf-token']).toBe('csrf');
    expect(headers['recaptcha-v2-response']).toBe('recaptcha');
    expect(headers['user-agent']).toBe('explorer-spec');
    expect(headers.authorization).toBe('Bearer token');
    expect(headers['show-scam-tokens']).toBe('true');
    expect(headers['api-v2-temp-token']).toBe('temp-token');
    expect(headers['updated-gas-oracle']).toBe('true');
    expect(headers['x-endpoint']).toBeUndefined();
    expect(headers['x-forwarded-for']).toBeUndefined();
  });

  it('sends a string body as it is', async() => {
    await fetchFactory(createRequest({}))(`${ origin }/api/v2/stats`, { method: 'POST', body: 'name=value' });

    expect(received[0].method).toBe('POST');
    expect(received[0].body).toBe('name=value');
  });

  it('serializes a parsed body back to json', async() => {
    const body = { address: '0x0000000000000000000000000000000000000001', amount: 2 };

    await fetchFactory(createRequest({}))(`${ origin }/api/v2/stats`, { method: 'POST', body: body as unknown as RequestInit['body'] });

    expect(received[0].method).toBe('POST');
    expect(JSON.parse(received[0].body)).toEqual(body);
  });

  it('sends no body when the caller gives none', async() => {
    await fetchFactory(createRequest({}))(`${ origin }/api/v2/stats`, { method: 'DELETE' });

    expect(received[0].method).toBe('DELETE');
    expect(received[0].body).toBe('');
  });
});
