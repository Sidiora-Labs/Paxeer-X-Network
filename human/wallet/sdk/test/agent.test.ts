import { randomBytes, randomUUID } from 'node:crypto';
import { readFileSync } from 'node:fs';
import type { AddressInfo } from 'node:net';
import { fileURLToPath } from 'node:url';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { ed25519 } from '@noble/curves/ed25519';
import {
  AGENT_BIND_MESSAGE_LENGTH,
  AGENT_REQUEST_HEADERS,
  GatewayRefusalError,
  InvalidParamsError,
  agentClaimDigest,
  agentDid,
  agentPublicKey,
  agentRequestDigest,
  agentRequestMethod,
  bindingMessage,
  buildClaim,
  claimAgent,
  signAgentRequest,
  signBinding,
} from '../src/index.js';
import { startPostgres, type EphemeralPostgres } from '../../gateway/test/support/postgres.js';
import { startIdentityProvider, type IdentityProvider } from '../../gateway/test/support/identity.js';

interface BindVector {
  name: string;
  chain_id: string;
  evm_address: string;
  nonce: string;
  message: string;
  signature: string;
  valid: boolean;
}

interface BindFixture {
  public_key: string;
  did: string;
  binds: BindVector[];
}

interface TestAgent {
  privateKey: Uint8Array;
  publicKey: Uint8Array;
  publicKeyHex: string;
  gatewayDid: string;
}

const bindFixture = JSON.parse(
  readFileSync(fileURLToPath(new URL('../../../../layerxproof/testdata/paxeer_bind_vectors.json', import.meta.url)), 'utf8'),
) as BindFixture;

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let base: string;
let verifyModule: typeof import('../../gateway/src/agent/verify.js');
let claimsModule: typeof import('../../gateway/src/routes/agents.js');
let agentsDb: typeof import('../../gateway/src/db/agents.js');
let poolModule: typeof import('../../gateway/src/db/pool.js');
let labelCounter = 0;

beforeAll(async () => {
  idp = await startIdentityProvider();
  process.env.NODE_ENV = 'test';
  process.env.LOG_LEVEL = 'error';
  process.env.SUPABASE_URL = idp.url;
  process.env.HYPERPAXEER_RPC_URL = `${idp.url}/rpc`;
  process.env.RPC_URLS = `${idp.url}/rpc`;
  process.env.WALLET_MASTER_KEY = randomBytes(32).toString('base64');
  process.env.CORS_ORIGINS = 'http://127.0.0.1:3000';
  process.env.AGENT_JWT_SECRET = randomBytes(32).toString('hex');
  process.env.AGENT_DEFAULT_FROZEN = 'false';
  pg = await startPostgres();
  verifyModule = await import('../../gateway/src/agent/verify.js');
  claimsModule = await import('../../gateway/src/routes/agents.js');
  agentsDb = await import('../../gateway/src/db/agents.js');
  poolModule = await import('../../gateway/src/db/pool.js');
  const index = await import('../../gateway/src/index.js');
  app = await index.buildApp();
  await app.listen({ host: '127.0.0.1', port: 0 });
  base = `http://127.0.0.1:${(app.server.address() as AddressInfo).port}`;
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
  await idp?.stop();
}, 120_000);

function hex(bytes: Uint8Array): string {
  return Buffer.from(bytes).toString('hex');
}

function newAgent(): TestAgent {
  const privateKey = ed25519.utils.randomPrivateKey();
  const publicKey = agentPublicKey(privateKey);
  const publicKeyHex = hex(publicKey);
  labelCounter += 1;
  return { privateKey, publicKey, publicKeyHex, gatewayDid: `did:matrix:sdk${labelCounter}:${publicKeyHex.slice(0, 16)}` };
}

async function registeredAgent(): Promise<TestAgent> {
  const agent = newAgent();
  const principal = await agentsDb.upsertPrincipalOnVerify({
    did: agent.gatewayDid,
    label: agent.gatewayDid.split(':')[2]!,
    keyFingerprint: agent.publicKeyHex.slice(0, 16),
    publicKey: agent.publicKeyHex,
  });
  expect(principal.is_frozen).toBe(false);
  return agent;
}

function nowSeconds(): number {
  return Math.floor(Date.now() / 1000);
}

async function verifySigned(headers: Record<string, string>, method: string, path: string, body: string) {
  return verifyModule.verifyAgentRequest({
    method: verifyModule.agentRequestMethod({ method, url: path }),
    headers,
    body: Buffer.from(body, 'utf8'),
  });
}

describe('agent request signing against the gateway verifier', () => {
  it('agent_request_digest_matches_gateway_bytes', () => {
    const nonce = Uint8Array.from({ length: 16 }, (_, i) => i + 1);
    const input = { method: 'POST /v1/agent/wallet/send', keyId: 'did:matrix:sdk:0011223344556677', expiry: 1_900_000_120n };
    for (const body of ['', '{"to":"0x01","value":"1"}']) {
      const sdk = agentRequestDigest({ ...input, nonce, body });
      const gateway = verifyModule.agentRequestDigest({ ...input, nonce: Buffer.from(nonce), body: Buffer.from(body, 'utf8') });
      expect(hex(sdk)).toBe(gateway.toString('hex'));
    }
    expect(agentRequestMethod('post', 'http://127.0.0.1:1/v1/agent/wallet/send?x=1')).toBe(
      verifyModule.agentRequestMethod({ method: 'post', url: '/v1/agent/wallet/send?x=1' }),
    );
    expect(agentRequestMethod('get', '/v1/agent/me?a=b#c')).toBe('GET /v1/agent/me');
    expect(() => agentRequestDigest({ ...input, nonce: new Uint8Array(15), body: '' })).toThrow(InvalidParamsError);
  });

  it('agent_request_valid_signature_accepted', async () => {
    const agent = await registeredAgent();
    const body = JSON.stringify({ to: '0x0000000000000000000000000000000000000001', value: '1' });
    const headers = signAgentRequest({
      privateKey: agent.privateKey,
      keyId: agent.gatewayDid,
      method: 'POST',
      url: `${base}/v1/agent/wallet/send?trace=1`,
      body,
    });
    expect(Object.keys(headers).sort()).toEqual(Object.values(AGENT_REQUEST_HEADERS).sort());
    expect(headers['x-agent-key']).toBe(agent.gatewayDid);
    expect(headers['x-agent-nonce']).toMatch(/^[0-9a-f]{32}$/);
    expect(headers['x-agent-expires']).toMatch(/^\d+$/);
    expect(headers['x-agent-signature']).toMatch(/^[0-9a-f]{128}$/);
    const verdict = await verifySigned({ ...headers }, 'POST', '/v1/agent/wallet/send?trace=1', body);
    expect(verdict.ok).toBe(true);
    if (verdict.ok) expect(verdict.principal.did).toBe(agent.gatewayDid);
  });

  it('agent_request_replayed_nonce_refused', async () => {
    const agent = await registeredAgent();
    const body = '{"n":1}';
    const headers = signAgentRequest({
      privateKey: agent.privateKey,
      keyId: agent.gatewayDid,
      method: 'POST',
      url: '/v1/agent/wallet/send',
      body,
      nonce: new Uint8Array(randomBytes(16)),
    });
    const first = await verifySigned({ ...headers }, 'POST', '/v1/agent/wallet/send', body);
    expect(first.ok).toBe(true);
    const second = await verifySigned({ ...headers }, 'POST', '/v1/agent/wallet/send', body);
    expect(second.ok).toBe(false);
    if (!second.ok) expect(second.refusal.code).toBe('agent_nonce_replayed');
  });

  it('agent_request_expired_refused', async () => {
    const agent = await registeredAgent();
    const body = '{"n":2}';
    const headers = signAgentRequest({
      privateKey: agent.privateKey,
      keyId: agent.gatewayDid,
      method: 'POST',
      url: '/v1/agent/wallet/send',
      body,
      expiresAt: nowSeconds() - 1,
    });
    const verdict = await verifySigned({ ...headers }, 'POST', '/v1/agent/wallet/send', body);
    expect(verdict.ok).toBe(false);
    if (!verdict.ok) expect(verdict.refusal.code).toBe('agent_request_expired');
  });

  it('agent_request_wrong_key_refused', async () => {
    const agent = await registeredAgent();
    const impostor = newAgent();
    const body = '{"n":3}';
    const headers = signAgentRequest({
      privateKey: impostor.privateKey,
      keyId: agent.gatewayDid,
      method: 'POST',
      url: '/v1/agent/wallet/send',
      body,
    });
    const verdict = await verifySigned({ ...headers }, 'POST', '/v1/agent/wallet/send', body);
    expect(verdict.ok).toBe(false);
    if (!verdict.ok) expect(verdict.refusal.code).toBe('agent_bad_signature');
  });

  it('agent_request_changed_body_refused', async () => {
    const agent = await registeredAgent();
    const headers = signAgentRequest({
      privateKey: agent.privateKey,
      keyId: agent.gatewayDid,
      method: 'POST',
      url: '/v1/agent/wallet/send',
      body: '{"value":"1"}',
    });
    const verdict = await verifySigned({ ...headers }, 'POST', '/v1/agent/wallet/send', '{"value":"2"}');
    expect(verdict.ok).toBe(false);
    if (!verdict.ok) expect(verdict.refusal.code).toBe('agent_bad_signature');
  });
});

describe('agent identity and binding', () => {
  it('agent_did_matches_derivation', async () => {
    expect(agentDid(Buffer.from(bindFixture.public_key, 'hex'))).toBe(bindFixture.did);
    const agent = await registeredAgent();
    const principal = await agentsDb.findPrincipal(agent.gatewayDid);
    expect(principal).not.toBeNull();
    expect(agentDid(agent.publicKey)).toBe(`did:layerx:${principal!.public_key}`);
    expect(agentDid(agent.publicKey)).toMatch(/^did:layerx:[0-9a-f]{64}$/);
    expect(() => agentDid(new Uint8Array(31))).toThrow(InvalidParamsError);
  });

  it('agent_binding_message_matches_lxwire_vectors', () => {
    const fixtureKey = Buffer.from(bindFixture.public_key, 'hex');
    expect(bindFixture.binds.length).toBeGreaterThan(0);
    for (const vector of bindFixture.binds) {
      const message = bindingMessage(BigInt(vector.chain_id), `0x${vector.evm_address}`, BigInt(vector.nonce));
      expect(message.length).toBe(AGENT_BIND_MESSAGE_LENGTH);
      expect(AGENT_BIND_MESSAGE_LENGTH).toBe(77);
      expect(hex(message)).toBe(vector.message);
      expect(ed25519.verify(vector.signature, message, fixtureKey)).toBe(vector.valid);
    }
    const agent = newAgent();
    const first = bindFixture.binds[0]!;
    const signed = signBinding({
      privateKey: agent.privateKey,
      chainId: BigInt(first.chain_id),
      evmAddress: Buffer.from(first.evm_address, 'hex'),
      nonce: BigInt(first.nonce),
    });
    expect(hex(signed.message)).toBe(first.message);
    expect(hex(signed.publicKey)).toBe(agent.publicKeyHex);
    expect(signed.did).toBe(agentDid(agent.publicKey));
    expect(ed25519.verify(signed.signature, signed.message, agent.publicKey)).toBe(true);
    expect(() => bindingMessage(125, '0x1234', 0)).toThrow(InvalidParamsError);
    expect(() => bindingMessage(125, `0x${first.evm_address}`, -1)).toThrow(InvalidParamsError);
  });
});

describe('agent claim flow against the gateway claim route', () => {
  it('agent_claim_counter_signature_accepted_by_gateway', async () => {
    const agent = await registeredAgent();
    const owner = randomUUID();
    const claim = buildClaim({ privateKey: agent.privateKey, did: agent.gatewayDid, ownerUserId: owner });
    expect(hex(agentClaimDigest(agent.gatewayDid, owner, BigInt(claim.expires)))).toBe(
      claimsModule.agentClaimDigest(agent.gatewayDid, owner, BigInt(claim.expires)).toString('hex'),
    );
    const token = await idp.mintUserToken(owner);
    const result = await claimAgent(globalThis.fetch, base, token, agent.gatewayDid, claim);
    expect(result).toEqual({ did: agent.gatewayDid, owner_user_id: owner });
    const principal = await agentsDb.findPrincipal(agent.gatewayDid);
    expect(principal!.owner_user_id).toBe(owner);

    const again = await claimAgent(globalThis.fetch, base, token, agent.gatewayDid, claim).catch((err: unknown) => err);
    expect(again).toBeInstanceOf(GatewayRefusalError);
    expect((again as GatewayRefusalError).status).toBe(409);
  });

  it('agent_claim_signed_by_another_key_refused', async () => {
    const agent = await registeredAgent();
    const impostor = newAgent();
    const owner = randomUUID();
    const claim = buildClaim({ privateKey: impostor.privateKey, did: agent.gatewayDid, ownerUserId: owner });
    const token = await idp.mintUserToken(owner);
    const refused = await claimAgent(globalThis.fetch, base, token, agent.gatewayDid, claim).catch((err: unknown) => err);
    expect(refused).toBeInstanceOf(GatewayRefusalError);
    expect((refused as GatewayRefusalError).status).toBe(401);
    expect(((refused as GatewayRefusalError).body as { error: string }).error).toBe('bad_claim_signature');
    const principal = await agentsDb.findPrincipal(agent.gatewayDid);
    expect(principal!.owner_user_id).toBeNull();
  });
});
