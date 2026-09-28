import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { createServer, type Server } from 'node:http';
import type { AddressInfo } from 'node:net';
import {
  RpcPool,
  RpcResponseError,
  RpcUnavailableError,
  rpcPoolFromConfig,
} from '../src/rpc/pool.js';
import { env } from '../src/env.js';

interface Node {
  server: Server;
  url: string;
  failing: boolean;
  head: number;
  calls: string[];
  revertCalls: boolean;
}

const nodes: Node[] = [];

function startNode(): Promise<Node> {
  const node: Node = { server: null as unknown as Server, url: '', failing: false, head: 1000, calls: [], revertCalls: false };
  node.server = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
      node.calls.push(body.method);
      if (node.failing) {
        res.writeHead(500, { 'content-type': 'text/plain' });
        res.end('upstream unavailable');
        return;
      }
      const reply = (payload: Record<string, unknown>): void => {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, ...payload }));
      };
      switch (body.method) {
        case 'eth_blockNumber':
          return reply({ result: `0x${node.head.toString(16)}` });
        case 'eth_chainId':
          return reply({ result: '0x7d' });
        case 'eth_getTransactionCount':
          return reply({ result: '0x9' });
        case 'eth_call':
          if (node.revertCalls) return reply({ error: { code: 3, message: 'execution reverted', data: '0x' } });
          return reply({ result: '0x' });
        case 'eth_estimateGas':
          return reply({ result: '0x5208' });
        case 'eth_maxPriorityFeePerGas':
          return reply({ result: '0x3b9aca00' });
        case 'eth_getBlockByNumber':
          return reply({
            result: {
              number: `0x${node.head.toString(16)}`,
              hash: `0x${'ab'.repeat(32)}`,
              parentHash: `0x${'cd'.repeat(32)}`,
              timestamp: '0x6500',
              gasLimit: '0x1c9c380',
              gasUsed: '0x0',
              baseFeePerGas: '0x77359400',
              transactions: [],
            },
          });
        case 'eth_sendRawTransaction':
          return reply({ result: `0x${'11'.repeat(32)}` });
        default:
          return reply({ error: { code: -32601, message: 'method not found' } });
      }
    });
  });
  return new Promise((r) =>
    node.server.listen(0, '127.0.0.1', () => {
      node.url = `http://127.0.0.1:${(node.server.address() as AddressInfo).port}`;
      nodes.push(node);
      r(node);
    }),
  );
}

let a: Node;
let b: Node;

function pool(lag = 20): RpcPool {
  return new RpcPool({ urls: [a.url, b.url], chainId: 125, lagThresholdBlocks: lag, timeoutMs: 3_000, healthIntervalMs: 60_000 });
}

beforeAll(async () => {
  a = await startNode();
  b = await startNode();
});

beforeEach(() => {
  for (const n of nodes) {
    n.failing = false;
    n.head = 1000;
    n.calls = [];
    n.revertCalls = false;
  }
});

afterAll(async () => {
  await Promise.all(nodes.map((n) => new Promise<void>((r) => n.server.close(() => r()))));
});

describe('RpcPool', () => {
  it('fails over from a failing endpoint and takes it back once it recovers', async () => {
    const p = pool();
    a.failing = true;
    expect(await p.getTransactionCount('0x00000000000000000000000000000000000000b1')).toBe(9);
    expect(a.calls).toContain('eth_getTransactionCount');
    expect(b.calls).toContain('eth_getTransactionCount');
    expect(p.status().find((s) => s.url === a.url)?.state).toBe('down');

    a.calls = [];
    b.calls = [];
    expect(await p.request('eth_chainId')).toBe('0x7d');
    expect(a.calls).toEqual([]);
    expect(b.calls).toEqual(['eth_chainId']);

    a.failing = false;
    const status = await p.checkHealth();
    expect(status.map((s) => s.state)).toEqual(['healthy', 'healthy']);
    expect(p.healthyCount()).toBe(2);

    b.failing = true;
    a.calls = [];
    const hash = await p.sendRawTransaction('0x02f86b');
    expect(hash).toBe(`0x${'11'.repeat(32)}`);
    expect(a.calls).toContain('eth_sendRawTransaction');
  });

  it('removes an endpoint that lags the best head and refuses rather than reading stale state', async () => {
    const p = pool(20);
    b.head = 900;
    const status = await p.checkHealth();
    expect(status.find((s) => s.url === b.url)?.state).toBe('lagging');
    expect(status.find((s) => s.url === a.url)?.state).toBe('healthy');
    b.calls = [];
    await p.request('eth_chainId');
    expect(b.calls).toEqual([]);

    a.failing = true;
    await expect(p.request('eth_chainId')).rejects.toBeInstanceOf(RpcUnavailableError);
    expect(b.calls).toEqual([]);

    a.failing = false;
    b.head = 995;
    await p.checkHealth();
    expect(p.healthyCount()).toBe(2);
  });

  it('surfaces a JSON-RPC error without failing over', async () => {
    const p = pool();
    await p.checkHealth();
    a.revertCalls = true;
    b.revertCalls = true;
    a.calls = [];
    b.calls = [];
    const err = await p
      .simulate({ from: '0x00000000000000000000000000000000000000b2', to: '0x00000000000000000000000000000000000000b3', data: '0x' })
      .catch((e: unknown) => e);
    expect(err).toBeInstanceOf(RpcResponseError);
    expect((err as RpcResponseError).code).toBe(3);
    expect(a.calls.length + b.calls.length).toBe(1);
    expect(p.healthyCount()).toBe(2);
  });

  it('estimates gas and fees through the pool', async () => {
    const p = pool();
    a.failing = true;
    const pinned = await p.prepareGas({
      from: '0x00000000000000000000000000000000000000b4',
      to: '0x00000000000000000000000000000000000000b5',
      value: 1n,
      maxFeePerGas: 7n,
      maxPriorityFeePerGas: 3n,
    });
    expect(pinned).toEqual({ gas: 25_200n, maxFeePerGas: 7n, maxPriorityFeePerGas: 3n });

    const live = await p.prepareGas({ from: '0x00000000000000000000000000000000000000b4', gas: 50_000n });
    expect(live.gas).toBe(50_000n);
    expect(live.maxPriorityFeePerGas).toBe(1_000_000_000n);
    expect(live.maxFeePerGas).toBeGreaterThan(2_000_000_000n);
    expect(b.calls).toContain('eth_getBlockByNumber');
  });

  it('defaults to the sixteen public hosts without contacting them', () => {
    const p = rpcPoolFromConfig(env);
    const urls = p.status().map((s) => s.url);
    expect(urls).toHaveLength(16);
    expect(urls[0]).toBe('https://api1.mainnet-beta.paxeer.network');
    expect(urls[15]).toBe('https://api16.mainnet-beta.paxeer.network');
    expect(p.status().every((s) => s.state === 'unknown' && s.checkedAt === null)).toBe(true);
  });
});
