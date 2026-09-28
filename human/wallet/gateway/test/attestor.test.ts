import { afterAll, beforeAll, beforeEach, describe, expect, it } from 'vitest';
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { createServer as createHttpServer, type Server as HttpServer } from 'node:http';
import { createServer as createHttpsServer, type Server as HttpsServer } from 'node:https';
import type { AddressInfo } from 'node:net';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';
import type { FastifyInstance } from 'fastify';
import { SignJWT, exportJWK, generateKeyPair, type KeyLike } from 'jose';
import {
  keccak256,
  parseTransaction,
  recoverMessageAddress,
  recoverTransactionAddress,
  recoverTypedDataAddress,
  type Hex,
  type TypedDataDefinition,
} from 'viem';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';

const here = dirname(fileURLToPath(import.meta.url));
const fixtureDir = join(here, 'fixtures', 'attestor');
const fixture = <T>(name: string): T => JSON.parse(readFileSync(join(fixtureDir, name), 'utf8')) as T;

interface RecordedRequest {
  key_id: string;
  kind: string;
  transaction?: string;
  typed_data?: string;
  message?: string;
}
interface RecordedResponse {
  node_id: string;
  key_id: string;
  kind: string;
  signed_bytes: string;
  signature: string;
  recovery_id: number;
  audit_sequence: number;
}
interface WireRequest extends RecordedRequest {
  session_id: string;
  signers: string[];
}
interface Received {
  path: string;
  authorization: string | undefined;
  body: WireRequest;
}

const walletFixture = fixture<{ key_id: string; address: `0x${string}`; chain_id: number }>('wallet.json');
const routeInputs = fixture<{
  tx: { to: string; value: string; gas: string; maxFeePerGas: string; maxPriorityFeePerGas: string };
  nonce: number;
  message: string;
  typed_data: TypedDataDefinition;
}>('route-inputs.json');
const recorded: Array<{ request: RecordedRequest; response: RecordedResponse }> = [
  'sign-evm-tx',
  'sign-personal-message',
  'sign-typed-data',
].map((n) => ({ request: fixture(`${n}.request.json`), response: fixture(`${n}.response.json`) }));
const healthTemplate = fixture<Record<string, unknown>>('health.json');
const keyTemplate = fixture<Record<string, unknown>>('keys.generate.response.json');
const goldenDir = join(here, '..', '..', 'schema', 'attestor-api', 'golden');
const golden = <T>(name: string): T => JSON.parse(readFileSync(join(goldenDir, name), 'utf8')) as T;

interface AttestorNode {
  nodeId: string;
  index: number;
  server: HttpsServer;
  url: string;
  ready: boolean;
  healthDelayMs: number;
  refuse: 'policy' | 'token' | null;
  truncate: boolean;
  received: Received[];
}

let tlsDir: string;
let tls: { caCert: string; serverCert: string; serverKey: string; clientCert: string; clientKey: string };
const attestorNodes: AttestorNode[] = [];
let expectedToken = '';

function makeCertificates(): void {
  tlsDir = mkdtempSync(join(tmpdir(), 'gateway-attestor-tls-'));
  const p = (n: string): string => join(tlsDir, n);
  const ossl = (args: string[]): void => {
    execFileSync('openssl', args, { stdio: 'pipe' });
  };
  const ec = ['-newkey', 'ec', '-pkeyopt', 'ec_paramgen_curve:prime256v1', '-nodes'];
  ossl(['req', '-x509', ...ec, '-keyout', p('ca.key'), '-out', p('ca.crt'), '-days', '1', '-subj', '/CN=attestor-test-ca']);
  writeFileSync(p('server.ext'), 'subjectAltName=IP:127.0.0.1\nextendedKeyUsage=serverAuth\n');
  writeFileSync(p('client.ext'), 'extendedKeyUsage=clientAuth\n');
  for (const [name, subj] of [
    ['server', '/CN=attestor'],
    ['client', '/CN=wallet-gateway'],
  ] as const) {
    ossl(['req', ...ec, '-keyout', p(`${name}.key`), '-out', p(`${name}.csr`), '-subj', subj]);
    ossl([
      'x509', '-req', '-in', p(`${name}.csr`), '-CA', p('ca.crt'), '-CAkey', p('ca.key'), '-CAcreateserial',
      '-out', p(`${name}.crt`), '-days', '1', '-extfile', p(`${name}.ext`),
    ]);
  }
  tls = {
    caCert: readFileSync(p('ca.crt'), 'utf8'),
    serverCert: readFileSync(p('server.crt'), 'utf8'),
    serverKey: readFileSync(p('server.key'), 'utf8'),
    clientCert: readFileSync(p('client.crt'), 'utf8'),
    clientKey: readFileSync(p('client.key'), 'utf8'),
  };
}

function strip(req: WireRequest): RecordedRequest {
  const { session_id: _session, signers: _signers, ...rest } = req;
  return rest;
}

function startAttestor(index: number): Promise<AttestorNode> {
  const node: AttestorNode = {
    nodeId: `attestor-${index}`,
    index,
    server: null as unknown as HttpsServer,
    url: '',
    ready: true,
    healthDelayMs: 0,
    refuse: null,
    truncate: false,
    received: [],
  };
  node.server = createHttpsServer(
    { key: tls.serverKey, cert: tls.serverCert, ca: tls.caCert, requestCert: true, rejectUnauthorized: true },
    (req, res) => {
      const chunks: Buffer[] = [];
      req.on('data', (c: Buffer) => chunks.push(c));
      req.on('end', () => {
        const send = (status: number, body: unknown): void => {
          res.writeHead(status, { 'content-type': 'application/json' });
          res.end(JSON.stringify(body));
        };
        const peers = Object.fromEntries(
          [1, 2, 3, 4, 5].filter((i) => i !== index).map((i) => [`attestor-${i}`, { reachable: true, rtt_ns: 1000 }]),
        );
        if (req.method === 'GET' && req.url === '/health') {
          setTimeout(
            () =>
              send(node.ready ? 200 : 503, {
                ...healthTemplate,
                node_id: node.nodeId,
                region: `region-${index}`,
                peers,
                reachable_peers: 4,
                ready: node.ready,
                ...(node.ready ? {} : { readiness_error: 'share store locked' }),
              }),
            node.healthDelayMs,
          );
          return;
        }
        if (req.method === 'POST' && (req.url === '/v1/sign' || req.url === '/v1/keys/generate')) {
          const wire = JSON.parse(Buffer.concat(chunks).toString('utf8')) as WireRequest;
          node.received.push({ path: req.url, authorization: req.headers.authorization, body: wire });
          if (req.url === '/v1/keys/generate') {
            send(200, { ...keyTemplate, node_id: node.nodeId, key_id: wire.key_id, audit_sequence: index });
            return;
          }
          if (node.refuse) {
            send(node.refuse === 'policy' ? 403 : 401, fixture(`refusal-${node.refuse}.response.json`));
            return;
          }
          if (req.headers.authorization !== `Bearer ${expectedToken}` || !wire.signers.includes(node.nodeId)) {
            send(401, fixture('refusal-token.response.json'));
            return;
          }
          const match = recorded.find((r) => JSON.stringify(r.request) === JSON.stringify(strip(wire)));
          if (!match) {
            send(400, { error: { category: 'session', code: 'session_bad_request', message: 'request does not match a recorded fixture' } });
            return;
          }
          send(200, {
            ...match.response,
            node_id: node.nodeId,
            signature: node.truncate ? match.response.signature.slice(0, 128) : match.response.signature,
            audit_sequence: match.response.audit_sequence + index,
          });
          return;
        }
        send(404, { error: { category: 'session', code: 'session_bad_request', message: 'unknown route' } });
      });
    },
  );
  return new Promise((r) =>
    node.server.listen(0, '127.0.0.1', () => {
      node.url = `https://127.0.0.1:${(node.server.address() as AddressInfo).port}`;
      attestorNodes.push(node);
      r(node);
    }),
  );
}

let jwksServer: HttpServer;
let supabaseUrl: string;
let signingKey: KeyLike;
const KID = 'attestor-test-key';

async function startJwks(): Promise<void> {
  const { publicKey, privateKey } = await generateKeyPair('ES256');
  signingKey = privateKey;
  const jwk = { ...(await exportJWK(publicKey)), kid: KID, alg: 'ES256', use: 'sig' };
  jwksServer = createHttpServer((req, res) => {
    if (req.url === '/auth/v1/.well-known/jwks.json') {
      res.writeHead(200, { 'content-type': 'application/json' });
      res.end(JSON.stringify({ keys: [jwk] }));
      return;
    }
    res.writeHead(404);
    res.end();
  });
  await new Promise<void>((r) => jwksServer.listen(0, '127.0.0.1', () => r()));
  supabaseUrl = `http://127.0.0.1:${(jwksServer.address() as AddressInfo).port}`;
}

function mintToken(sub: string): Promise<string> {
  return new SignJWT({ email: `${sub.slice(0, 8)}@example.com` })
    .setProtectedHeader({ alg: 'ES256', kid: KID })
    .setIssuer(`${supabaseUrl}/auth/v1`)
    .setAudience('authenticated')
    .setSubject(sub)
    .setIssuedAt()
    .setExpirationTime('10m')
    .sign(signingKey);
}

let rpcServer: HttpServer;
let rpcUrl: string;
const broadcasts: Hex[] = [];

function startRpc(): Promise<void> {
  rpcServer = createHttpServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      const body = JSON.parse(Buffer.concat(chunks).toString('utf8')) as { id: number; method: string; params: unknown[] };
      const reply = (payload: Record<string, unknown>): void => {
        res.writeHead(200, { 'content-type': 'application/json' });
        res.end(JSON.stringify({ jsonrpc: '2.0', id: body.id, ...payload }));
      };
      switch (body.method) {
        case 'eth_blockNumber':
          return reply({ result: '0x3e8' });
        case 'eth_call':
          return reply({ result: '0x' });
        case 'eth_estimateGas':
          return reply({ result: '0x5208' });
        case 'eth_getTransactionCount':
          return reply({ result: `0x${routeInputs.nonce.toString(16)}` });
        case 'eth_sendRawTransaction': {
          const raw = body.params[0] as Hex;
          broadcasts.push(raw);
          return reply({ result: keccak256(raw) });
        }
        default:
          return reply({ error: { code: -32601, message: 'method not found' } });
      }
    });
  });
  return new Promise((r) =>
    rpcServer.listen(0, '127.0.0.1', () => {
      rpcUrl = `http://127.0.0.1:${(rpcServer.address() as AddressInfo).port}`;
      r();
    }),
  );
}

type ClientModule = typeof import('../src/attestor/client.js');
type QuorumModule = typeof import('../src/attestor/quorum.js');
let clientMod: ClientModule;
let quorumMod: QuorumModule;
let pgServer: EphemeralPostgres;

function newClient(): InstanceType<ClientModule['AttestorClient']> {
  return new clientMod.AttestorClient({
    endpoints: attestorNodes.map((n) => n.url),
    tls: { cert: tls.clientCert, key: tls.clientKey, ca: tls.caCert },
    quorum: 3,
    healthIntervalMs: 60_000,
    timeoutMs: 5_000,
  });
}

beforeAll(async () => {
  makeCertificates();
  for (let i = 1; i <= 5; i++) await startAttestor(i);
  await startJwks();
  await startRpc();
  process.env.SUPABASE_URL = supabaseUrl;
  pgServer = await startPostgres();
  clientMod = await import('../src/attestor/client.js');
  quorumMod = await import('../src/attestor/quorum.js');
}, 120_000);

afterAll(async () => {
  await Promise.all(attestorNodes.map((n) => new Promise<void>((r) => n.server.close(() => r()))));
  await new Promise<void>((r) => (jwksServer ? jwksServer.close(() => r()) : r()));
  await new Promise<void>((r) => (rpcServer ? rpcServer.close(() => r()) : r()));
  const { closePool } = await import('../src/db/pool.js');
  await closePool();
  await pgServer?.stop();
  if (tlsDir) rmSync(tlsDir, { recursive: true, force: true });
});

beforeEach(() => {
  for (const n of attestorNodes) {
    n.ready = true;
    n.healthDelayMs = 0;
    n.refuse = null;
    n.truncate = false;
    n.received = [];
  }
});

describe('selectQuorum', () => {
  const report = (id: string, ready = true, peers = 4) => ({
    node_id: id,
    region: 'r',
    share_count: 2,
    refresh_epoch: 1,
    audit_sequence: 0,
    audit_head: '',
    peers: {},
    reachable_peers: peers,
    ready,
  });
  const node = (id: string, latencyMs: number | null, ready = true, peers = 4) => ({
    endpoint: `https://127.0.0.1/${id}`,
    nodeId: id,
    healthy: ready && latencyMs !== null,
    latencyMs,
    checkedAt: 1,
    report: report(id, ready, peers),
    lastError: null,
  });

  it('picks the three lowest-latency healthy nodes', () => {
    const picked = quorumMod.selectQuorum(
      [node('a', 40), node('b', 5), node('c', null), node('d', 12, false), node('e', 20), node('f', 30, true, 1)],
      3,
    );
    expect(picked.map((m) => m.nodeId)).toEqual(['b', 'e', 'a']);
  });

  it('refuses when fewer than three healthy nodes remain', () => {
    expect(() => quorumMod.selectQuorum([node('a', 1), node('b', 2), node('c', 3, false)], 3)).toThrow(
      quorumMod.QuorumUnavailableError,
    );
  });
});

describe('AttestorClient over mutual TLS', () => {
  it('selects a quorum by health and latency with one unhealthy node', async () => {
    attestorNodes[1]!.ready = false;
    attestorNodes[4]!.healthDelayMs = 300;
    const client = newClient();
    try {
      const health = await client.refreshHealth();
      expect(health.map((h) => h.nodeId)).toEqual(attestorNodes.map((n) => n.nodeId));
      expect(health[1]!.healthy).toBe(false);
      expect(health[1]!.lastError).toBe('share store locked');
      expect(health.filter((h) => h.healthy)).toHaveLength(4);
      const quorum = client.selectQuorum();
      expect(quorum.map((m) => m.nodeId).sort()).toEqual(['attestor-1', 'attestor-3', 'attestor-4']);
    } finally {
      client.stop();
    }
  });

  it('posts one session to exactly three participants and collects signature and audit sequences', async () => {
    attestorNodes[1]!.ready = false;
    attestorNodes[4]!.healthDelayMs = 300;
    expectedToken = 'session-token';
    const client = newClient();
    try {
      await client.refreshHealth();
      const req = recorded[1]!.request;
      const result = await clientMod.signThroughAttestors(client, {
        keyId: req.key_id,
        payload: { kind: 'personal_message', message: `0x${req.message!}` },
        authorisation: { scheme: 'supabase_jwt', token: expectedToken },
      });
      const hit = attestorNodes.filter((n) => n.received.length > 0);
      expect(hit.map((n) => n.nodeId).sort()).toEqual(['attestor-1', 'attestor-3', 'attestor-4']);
      expect(hit.every((n) => n.received[0]!.path === '/v1/sign')).toBe(true);
      expect(hit.every((n) => n.received[0]!.authorization === `Bearer ${expectedToken}`)).toBe(true);
      const bodies = hit.map((n) => n.received[0]!.body);
      expect(new Set(bodies.map((b) => b.session_id)).size).toBe(1);
      expect(new Set(bodies.map((b) => JSON.stringify(b))).size).toBe(1);
      const goldenKeys = Object.keys(golden<Record<string, unknown>>('sign.request.json')).filter((k) => k !== 'transaction');
      expect(Object.keys(bodies[0]!).sort()).toEqual([...goldenKeys, 'message'].sort());
      expect(bodies[0]!.session_id).toBe(result.sessionId);
      expect(bodies[0]!.signers).toEqual(result.participants);
      expect(result.signature).toBe(`0x${recorded[1]!.response.signature}`);
      expect(result.signature.length).toBe(2 + 65 * 2);
      expect(result.recoveryId).toBe(recorded[1]!.response.recovery_id);
      expect(result.signedBytes).toBe(`0x${recorded[1]!.response.signed_bytes}`);
      expect(result.audit.sort((x, y) => x.audit_sequence - y.audit_sequence)).toEqual([
        { node_id: 'attestor-1', audit_sequence: 201 },
        { node_id: 'attestor-3', audit_sequence: 203 },
        { node_id: 'attestor-4', audit_sequence: 204 },
      ]);
    } finally {
      client.stop();
    }
  });

  it('surfaces policy, token and quorum refusals as typed errors', async () => {
    expectedToken = 'session-token';
    const client = newClient();
    const input = {
      keyId: recorded[1]!.request.key_id,
      payload: { kind: 'personal_message' as const, message: `0x${recorded[1]!.request.message!}` as Hex },
      authorisation: { scheme: 'supabase_jwt' as const, token: expectedToken },
    };
    try {
      await client.refreshHealth();
      for (const n of attestorNodes) n.refuse = 'policy';
      const policy = await client.sign(input).catch((e: unknown) => e);
      expect(policy).toBeInstanceOf(clientMod.AttestorPolicyError);
      const policyGolden = golden<{ error: { code: string; policy_code: string } }>('error.policy.json');
      expect((policy as InstanceType<ClientModule['AttestorPolicyError']>).code).toBe(policyGolden.error.code);
      expect((policy as InstanceType<ClientModule['AttestorPolicyError']>).policyCode).toBe(policyGolden.error.policy_code);
      expect(clientMod.attestorErrorStatus(policy as InstanceType<ClientModule['AttestorError']>)).toBe(403);

      for (const n of attestorNodes) n.refuse = null;
      const token = await client.sign({ ...input, authorisation: { scheme: 'supabase_jwt', token: 'forged' } }).catch((e: unknown) => e);
      expect(token).toBeInstanceOf(clientMod.AttestorTokenError);
      expect((token as InstanceType<ClientModule['AttestorTokenError']>).code).toBe('token_missing');
      expect(clientMod.attestorErrorStatus(token as InstanceType<ClientModule['AttestorError']>)).toBe(401);

      for (const n of attestorNodes) n.truncate = true;
      const truncated = await client.sign(input).catch((e: unknown) => e);
      expect(truncated).toBeInstanceOf(clientMod.AttestorSessionError);
      expect((truncated as InstanceType<ClientModule['AttestorSessionError']>).code).toBe('bad_signature_length');
      for (const n of attestorNodes) n.truncate = false;

      attestorNodes[0]!.ready = false;
      attestorNodes[1]!.ready = false;
      attestorNodes[2]!.ready = false;
      await client.refreshHealth();
      const quorum = await client.sign(input).catch((e: unknown) => e);
      expect(quorum).toBeInstanceOf(clientMod.AttestorQuorumError);
      expect((quorum as InstanceType<ClientModule['AttestorQuorumError']>).code).toBe('quorum_unavailable');
      expect(clientMod.attestorErrorStatus(quorum as InstanceType<ClientModule['AttestorError']>)).toBe(503);
    } finally {
      client.stop();
    }
  });

  it('generates a key on every participant with the schema request body', async () => {
    const client = newClient();
    try {
      await client.refreshHealth();
      const key = await client.generateKey({ keyId: 'key-evm', curve: 'secp256k1', owner: 'user-0001' });
      const bodies = attestorNodes.map((n) => n.received[0]!);
      expect(bodies.every((b) => b.path === '/v1/keys/generate')).toBe(true);
      expect(new Set(bodies.map((b) => JSON.stringify(b.body))).size).toBe(1);
      const goldenKeys = Object.keys(golden<Record<string, unknown>>('keys.generate.request.json')).filter((k) => k !== 'account');
      expect(Object.keys(bodies[0]!.body).sort()).toEqual(goldenKeys.sort());
      expect(bodies[0]!.body.session_id).toBe(key.sessionId);
      expect(key.publicKey).toBe(`0x${keyTemplate.public_key as string}`);
      expect(key.address).toBe(keyTemplate.address);
      expect(key.refreshed).toBe(true);
      expect(key.audit.map((a) => a.audit_sequence)).toEqual([1, 2, 3, 4, 5]);
    } finally {
      client.stop();
    }
  });

  it('refuses a client that presents no certificate', async () => {
    const bare = new clientMod.AttestorClient({
      endpoints: attestorNodes.map((n) => n.url),
      tls: { cert: '', key: '', ca: tls.caCert },
      quorum: 3,
      healthIntervalMs: 60_000,
      timeoutMs: 5_000,
    });
    try {
      const health = await bare.refreshHealth();
      expect(health.every((h) => !h.healthy && h.lastError !== null)).toBe(true);
      await expect(
        bare.sign({
          keyId: recorded[1]!.request.key_id,
          payload: { kind: 'personal_message', message: `0x${recorded[1]!.request.message!}` },
          authorisation: { scheme: 'supabase_jwt', token: 'session-token' },
        }),
      ).rejects.toBeInstanceOf(clientMod.AttestorQuorumError);
      expect(attestorNodes.every((n) => n.received.length === 0)).toBe(true);
    } finally {
      bare.stop();
    }
  });
});

describe('signing routes', () => {
  type SignRoutes = typeof import('../src/routes/sign.js');
  type Wallets = typeof import('../src/db/wallets.js');
  type Audit = typeof import('../src/audit.js');
  let signMod: SignRoutes;
  let walletsMod: Wallets;
  let auditMod: Audit;
  let getPool: () => import('pg').Pool;
  let rpc: InstanceType<typeof import('../src/rpc/pool.js')['RpcPool']>;
  let nonces: InstanceType<typeof import('../src/nonce/store.js')['NonceStore']>;
  let client: InstanceType<ClientModule['AttestorClient']>;

  async function buildApp(limits: { client: number; account: number }): Promise<FastifyInstance> {
    const Fastify = (await import('fastify')).default;
    const app = Fastify({ logger: false });
    await app.register(signMod.signRoutes, {
      pool: getPool(),
      attestors: client,
      rpc,
      nonces,
      limiter: new auditMod.RateLimiter({ pool: getPool(), clientPerMinute: limits.client, accountPerMinute: limits.account }),
    });
    await app.ready();
    return app;
  }

  async function provision(migrated: boolean): Promise<{ userId: string; token: string; address: `0x${string}` }> {
    const userId = randomUUID();
    const { row } = await walletsMod.provisionWalletForUser(userId, 'standard');
    if (migrated) await migrate(userId);
    const token = await mintToken(userId);
    return { userId, token, address: migrated ? walletFixture.address : row.address };
  }

  async function migrate(userId: string): Promise<void> {
    await getPool().query(
      `update wallets set address = $2, migrated_at = now(), attestor_key_id = $3 where user_id = $1 and kind = 'standard'`,
      [userId, walletFixture.address, walletFixture.key_id],
    );
  }

  async function auditFor(requestId: string) {
    return auditMod.auditRowsForRequest(getPool(), requestId);
  }

  beforeAll(async () => {
    signMod = await import('../src/routes/sign.js');
    walletsMod = await import('../src/db/wallets.js');
    auditMod = await import('../src/audit.js');
    getPool = (await import('../src/db/pool.js')).getPool;
    const { RpcPool } = await import('../src/rpc/pool.js');
    const { NonceStore } = await import('../src/nonce/store.js');
    rpc = new RpcPool({ urls: [rpcUrl], chainId: 125, lagThresholdBlocks: 20, timeoutMs: 5_000, healthIntervalMs: 60_000 });
    nonces = new NonceStore({ pool: getPool(), chainId: 125, pendingCount: (a) => rpc.getTransactionCount(a, 'pending') });
    client = newClient();
  });

  afterAll(() => {
    client?.stop();
  });

  beforeEach(async () => {
    await getPool().query(
      `update wallets
          set migrated_at = null, attestor_key_id = null,
              address = '0x' || substr(md5(random()::text) || md5(random()::text), 1, 40)
        where address = $1`,
      [walletFixture.address],
    );
    await getPool().query('delete from nonce_allocations where address = $1', [walletFixture.address.toLowerCase()]);
    await client.refreshHealth();
    broadcasts.length = 0;
  });

  it('signs through the attestors for a migrated wallet and the envelope path otherwise, switching on the flag', async () => {
    const app = await buildApp({ client: 100, account: 100 });
    try {
      const user = await provision(false);
      expectedToken = user.token;
      const headers = { authorization: `Bearer ${user.token}` };

      const envelope = await app.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload: { message: routeInputs.message } });
      expect(envelope.statusCode).toBe(200);
      const envSig = envelope.json<{ signature: Hex; address: string }>();
      expect(envSig.address).toBe(user.address);
      expect(await recoverMessageAddress({ message: routeInputs.message, signature: envSig.signature })).toBe(user.address);
      expect(attestorNodes.every((n) => n.received.length === 0)).toBe(true);
      const envAudit = await auditFor(envelope.headers['x-request-id'] as string);
      expect(envAudit).toHaveLength(1);
      expect(envAudit[0]).toMatchObject({ path: 'envelope', decision: 'signed', kind: 'message', attestor_audit: [] });

      await migrate(user.userId);

      const threshold = await app.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload: { message: routeInputs.message } });
      expect(threshold.statusCode).toBe(200);
      const thSig = threshold.json<{ signature: Hex; address: string }>();
      expect(thSig.address).toBe(walletFixture.address);
      expect(await recoverMessageAddress({ message: routeInputs.message, signature: thSig.signature })).toBe(walletFixture.address);
      expect(attestorNodes.filter((n) => n.received.length === 1)).toHaveLength(3);
      const thAudit = await auditFor(threshold.headers['x-request-id'] as string);
      expect(thAudit).toHaveLength(1);
      expect(thAudit[0]!.path).toBe('attestor');
      expect(thAudit[0]!.decision).toBe('signed');
      expect(thAudit[0]!.session_id).toBe(attestorNodes.find((n) => n.received.length === 1)!.received[0]!.body.session_id);
      expect(thAudit[0]!.attestor_audit).toHaveLength(3);
      expect(thAudit[0]!.attestor_audit.every((a) => a.audit_sequence > 200 && a.audit_sequence <= 205)).toBe(true);
    } finally {
      await app.close();
    }
  });

  it('signs and broadcasts a transaction and signs typed data through the attestors', async () => {
    const app = await buildApp({ client: 100, account: 100 });
    try {
      const user = await provision(true);
      expectedToken = user.token;
      const headers = { authorization: `Bearer ${user.token}` };

      const signed = await app.inject({
        method: 'POST',
        url: '/v1/wallet/sign',
        headers,
        payload: { tx: { ...routeInputs.tx, nonce: routeInputs.nonce } },
      });
      expect(signed.statusCode).toBe(200);
      const signedTx = signed.json<{ signed_tx: Hex; chain_id: number }>().signed_tx;
      expect(await recoverTransactionAddress({ serializedTransaction: signedTx as never })).toBe(walletFixture.address);
      const parsed = parseTransaction(signedTx);
      expect(parsed.nonce).toBe(routeInputs.nonce);
      expect(parsed.chainId).toBe(125);

      const sent = await app.inject({ method: 'POST', url: '/v1/wallet/send', headers, payload: { tx: routeInputs.tx } });
      expect(sent.statusCode).toBe(200);
      expect(broadcasts).toEqual([signedTx]);
      expect(sent.json<{ tx_hash: Hex }>().tx_hash).toBe(keccak256(signedTx));
      const sendAudit = await auditFor(sent.headers['x-request-id'] as string);
      expect(sendAudit[0]).toMatchObject({ path: 'attestor', decision: 'broadcast', nonce: String(routeInputs.nonce), tx_hash: keccak256(signedTx) });
      const { rows } = await getPool().query<{ next_nonce: string }>(
        'select next_nonce::text from nonce_allocations where address = $1',
        [walletFixture.address.toLowerCase()],
      );
      expect(rows[0]?.next_nonce).toBe(String(routeInputs.nonce + 1));

      const typed = await app.inject({
        method: 'POST',
        url: '/v1/wallet/sign-typed-data',
        headers,
        payload: { typedData: routeInputs.typed_data },
      });
      expect(typed.statusCode).toBe(200);
      const typedSig = typed.json<{ signature: Hex }>().signature;
      expect(await recoverTypedDataAddress({ ...routeInputs.typed_data, signature: typedSig } as never)).toBe(walletFixture.address);
    } finally {
      await app.close();
    }
  });

  it('maps attestor refusals to typed HTTP responses and audits them', async () => {
    const app = await buildApp({ client: 100, account: 100 });
    try {
      const user = await provision(false);
      await migrate(user.userId);
      expectedToken = user.token;
      const headers = { authorization: `Bearer ${user.token}` };

      for (const n of attestorNodes) n.refuse = 'policy';
      const policy = await app.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload: { message: routeInputs.message } });
      expect(policy.statusCode).toBe(403);
      expect(policy.json()).toMatchObject({
        error: 'attestor_policy_refused',
        category: 'policy',
        code: 'policy_denied',
        policy_code: 'value_cap',
      });
      const policyAudit = await auditFor(policy.json<{ request_id: string }>().request_id);
      expect(policyAudit[0]).toMatchObject({ decision: 'refused', path: 'attestor', reason_code: 'policy:policy_denied' });

      for (const n of attestorNodes) n.refuse = null;
      expectedToken = 'a-different-token';
      const token = await app.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload: { message: routeInputs.message } });
      expect(token.statusCode).toBe(401);
      expect(token.json()).toMatchObject({ category: 'token', code: 'token_missing' });
    } finally {
      await app.close();
    }
  });

  it('rate-limits per client and per account with typed 429s and audit rows', async () => {
    const perClient = await buildApp({ client: 2, account: 100 });
    try {
      const user = await provision(false);
      const headers = { authorization: `Bearer ${user.token}` };
      const payload = { message: routeInputs.message };
      expect((await perClient.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload })).statusCode).toBe(200);
      expect((await perClient.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload })).statusCode).toBe(200);
      const limited = await perClient.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload });
      expect(limited.statusCode).toBe(429);
      expect(limited.json()).toMatchObject({ error: 'rate_limited', scope: 'client', limit_per_minute: 2 });
      expect(Number(limited.headers['retry-after'])).toBeGreaterThanOrEqual(1);
      const rows = await auditFor(limited.json<{ request_id: string }>().request_id);
      expect(rows[0]).toMatchObject({ decision: 'refused', path: 'none', reason_code: 'rate_limited_client', client_subject: user.userId });
    } finally {
      await perClient.close();
    }

    const perAccount = await buildApp({ client: 100, account: 1 });
    try {
      const user = await provision(false);
      const headers = { authorization: `Bearer ${user.token}` };
      const payload = { message: routeInputs.message };
      expect((await perAccount.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload })).statusCode).toBe(200);
      const limited = await perAccount.inject({ method: 'POST', url: '/v1/wallet/sign-message', headers, payload });
      expect(limited.statusCode).toBe(429);
      expect(limited.json()).toMatchObject({ error: 'rate_limited', scope: 'account', limit_per_minute: 1 });
      const rows = await auditFor(limited.json<{ request_id: string }>().request_id);
      expect(rows[0]).toMatchObject({
        decision: 'refused',
        path: 'envelope',
        reason_code: 'rate_limited_account',
        account: user.address.toLowerCase(),
      });
    } finally {
      await perAccount.close();
    }
  });
});
