import { createHash, randomBytes, randomUUID } from 'node:crypto';
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import type { FastifyInstance } from 'fastify';
import { verifyMessage } from 'viem';
import { startPostgres, type EphemeralPostgres } from './support/postgres.js';
import { newAgentKey, startIdentityProvider, type AgentKey, type IdentityProvider } from './support/identity.js';

let idp: IdentityProvider;
let pg: EphemeralPostgres;
let app: FastifyInstance;
let verifyModule: typeof import('../src/agent/verify.js');
let agentsModule: typeof import('../src/routes/agents.js');
let policyModule: typeof import('../src/policy/agent.js');
let poolModule: typeof import('../src/db/pool.js');

beforeAll(async () => {
  idp = await startIdentityProvider();
  process.env.SUPABASE_URL = idp.url;
  process.env.AGENT_JWT_SECRET = randomBytes(32).toString('hex');
  process.env.LOG_LEVEL = 'error';
  process.env.RPC_URLS = `${idp.url}/rpc`;
  pg = await startPostgres();
  const index = await import('../src/index.js');
  verifyModule = await import('../src/agent/verify.js');
  agentsModule = await import('../src/routes/agents.js');
  policyModule = await import('../src/policy/agent.js');
  poolModule = await import('../src/db/pool.js');
  app = await index.buildApp();
  await app.ready();
}, 120_000);

afterAll(async () => {
  await app?.close();
  await poolModule?.closePool();
  await pg?.stop();
  await idp?.stop();
}, 120_000);

async function authenticate(agent: AgentKey): Promise<{ token: string; owner: string | null; frozen: boolean }> {
  const challenge = await app.inject({
    method: 'POST',
    url: '/v1/agent/auth/challenge',
    payload: { did: agent.did },
  });
  expect(challenge.statusCode).toBe(200);
  const { nonce, message } = challenge.json() as { nonce: string; message: string };
  const verify = await app.inject({
    method: 'POST',
    url: '/v1/agent/auth/verify',
    payload: {
      did: agent.did,
      public_key: agent.publicKeyHex,
      nonce,
      signature: agent.sign(Buffer.from(message, 'utf8')),
    },
  });
  expect(verify.statusCode).toBe(200);
  const body = verify.json() as {
    token: string;
    scope: string;
    owner_user_id: string | null;
    is_frozen: boolean;
  };
  expect(body.scope).toBe('read');
  return { token: body.token, owner: body.owner_user_id, frozen: body.is_frozen };
}

function claimSignature(agent: AgentKey, ownerUserId: string, expiry: bigint): string {
  return agent.sign(agentsModule.agentClaimDigest(agent.did, ownerUserId, expiry));
}

async function claim(agent: AgentKey, ownerUserId: string, signature?: string, expires?: bigint) {
  const expiry = expires ?? BigInt(Math.floor(Date.now() / 1000) + 60);
  return app.inject({
    method: 'POST',
    url: `/v1/agents/${encodeURIComponent(agent.did)}/claim`,
    headers: { authorization: `Bearer ${await idp.mintUserToken(ownerUserId)}` },
    payload: { expires: expiry.toString(), signature: signature ?? claimSignature(agent, ownerUserId, expiry) },
  });
}

interface SignedOptions {
  nonce?: Buffer;
  expiry?: bigint;
  signedBody?: string;
  signer?: AgentKey;
}

function signedHeaders(agent: AgentKey, method: string, url: string, body: string, opts: SignedOptions = {}) {
  const nonce = opts.nonce ?? randomBytes(16);
  const expiry = opts.expiry ?? BigInt(Math.floor(Date.now() / 1000) + 60);
  const digest = verifyModule.agentRequestDigest({
    method: `${method} ${url}`,
    keyId: agent.did,
    nonce,
    expiry,
    body: Buffer.from(opts.signedBody ?? body, 'utf8'),
  });
  return {
    'content-type': 'application/json',
    'x-agent-key': agent.did,
    'x-agent-nonce': nonce.toString('hex'),
    'x-agent-expires': expiry.toString(),
    'x-agent-signature': (opts.signer ?? agent).sign(digest),
  };
}

async function signedPost(agent: AgentKey, url: string, payload: unknown, opts: SignedOptions = {}) {
  const body = JSON.stringify(payload);
  return app.inject({
    method: 'POST',
    url,
    headers: signedHeaders(agent, 'POST', url, body, opts),
    payload: body,
  });
}

async function ownerCall(ownerUserId: string, method: 'POST' | 'PUT', url: string, payload?: unknown) {
  return app.inject({
    method,
    url,
    headers: { authorization: `Bearer ${await idp.mintUserToken(ownerUserId)}` },
    ...(payload === undefined ? {} : { payload: payload as Record<string, unknown> }),
  });
}

describe('agent request digest', () => {
  it('matches the attestor layout byte for byte', () => {
    const nonce = Buffer.from(Array.from({ length: 16 }, (_, i) => i + 1));
    const body = Buffer.from('payload', 'utf8');
    const got = verifyModule.agentRequestDigest({
      method: 'sign',
      keyId: 'key-9',
      nonce,
      expiry: 1_900_000_120n,
      body,
    });
    const u32 = (n: number) => {
      const b = Buffer.alloc(4);
      b.writeUInt32BE(n);
      return b;
    };
    const exp = Buffer.alloc(8);
    exp.writeBigUInt64BE(1_900_000_120n);
    const want = createHash('sha256')
      .update(
        Buffer.concat([
          Buffer.from('PXW:AGENT-REQUEST:v1'),
          u32(4),
          Buffer.from('sign'),
          u32(5),
          Buffer.from('key-9'),
          nonce,
          exp,
          createHash('sha256').update(body).digest(),
        ]),
      )
      .digest();
    expect(got.equals(want)).toBe(true);

    const shifted = verifyModule.agentRequestDigest({
      method: 'sig',
      keyId: 'nkey-9',
      nonce,
      expiry: 1_900_000_120n,
      body,
    });
    expect(shifted.equals(got)).toBe(false);
  });

  it('refuses a nonce that is not sixteen bytes', () => {
    expect(() =>
      verifyModule.agentRequestDigest({
        method: 'POST /v1/agent/send',
        keyId: 'k',
        nonce: Buffer.alloc(15),
        expiry: 1n,
        body: Buffer.alloc(0),
      }),
    ).toThrow(RangeError);
  });
});

describe('decodeAddressArg', () => {
  it('reads a left-padded address word and refuses dirty or short words', () => {
    const recipient = '00000000000000000000000011112222333344445555666677778888aaaabbbb';
    const amount = '0000000000000000000000000000000000000000000000000000000000000001';
    const data = `${policyModule.ERC20_TRANSFER_SELECTOR}${recipient}${amount}`;
    expect(policyModule.decodeAddressArg(data, 0)).toBe('0x11112222333344445555666677778888aaaabbbb');
    expect(policyModule.decodeAddressArg(data, 2)).toBeNull();
    const dirty = `${policyModule.ERC20_APPROVE_SELECTOR}ff${recipient.slice(2)}${amount}`;
    expect(policyModule.decodeAddressArg(dirty, 0)).toBeNull();
  });
});

describe('agent consent', () => {
  it('does not bind an owner at verify even when the DID label is a user id', async () => {
    const victim = randomUUID();
    const agent = newAgentKey(victim);
    const session = await authenticate(agent);
    expect(session.owner).toBeNull();
    const { rows } = await poolModule.query<{ owner_user_id: string | null }>(
      'select owner_user_id from agent_principals where did = $1',
      [agent.did],
    );
    expect(rows[0]?.owner_user_id).toBeNull();
  });

  it('records ownership only with the agent counter-signature and only once', async () => {
    const agent = newAgentKey('claimable');
    await authenticate(agent);
    const owner = randomUUID();
    const intruder = randomUUID();

    const forged = await claim(agent, owner, claimSignature(agent, intruder, BigInt(Math.floor(Date.now() / 1000) + 60)));
    expect(forged.statusCode).toBe(401);
    expect(forged.json()).toMatchObject({ error: 'bad_claim_signature' });

    const stale = await claim(agent, owner, undefined, BigInt(Math.floor(Date.now() / 1000) - 1));
    expect(stale.statusCode).toBe(401);
    expect(stale.json()).toMatchObject({ error: 'claim_expired' });

    const far = await claim(agent, owner, undefined, BigInt(Math.floor(Date.now() / 1000) + 3_600));
    expect(far.statusCode).toBe(401);
    expect(far.json()).toMatchObject({ error: 'claim_expiry_too_far' });

    const ok = await claim(agent, owner);
    expect(ok.statusCode).toBe(200);
    expect(ok.json()).toEqual({ did: agent.did, owner_user_id: owner });

    const second = await claim(agent, intruder);
    expect(second.statusCode).toBe(409);
    expect(second.json()).toMatchObject({ error: 'already_owned' });

    const unknown = await claim(newAgentKey('never-seen'), owner);
    expect(unknown.statusCode).toBe(404);
  });
});

describe('agent request signing', () => {
  let agent: AgentKey;
  let token: string;
  let owner: string;

  beforeAll(async () => {
    agent = newAgentKey('signer');
    const session = await authenticate(agent);
    token = session.token;
    expect(session.frozen).toBe(true);
    owner = randomUUID();
    expect((await claim(agent, owner)).statusCode).toBe(200);
  }, 60_000);

  it('lets the token read', async () => {
    const res = await app.inject({
      method: 'GET',
      url: '/v1/agent/me',
      headers: { authorization: `Bearer ${token}` },
    });
    expect(res.statusCode).toBe(200);
    expect(res.json()).toMatchObject({ did: agent.did, owner_user_id: owner });
  });

  it.each([
    ['/v1/agent/send', { tx: { to: '0x000000000000000000000000000000000000dEaD', value: '1' } }],
    ['/v1/agent/actions/layerx/deposit', { amount: '1' }],
    ['/v1/agent/precompiles/streams/settle', { stream_id: '1' }],
  ])('refuses the token on write route %s', async (url, payload) => {
    const res = await app.inject({
      method: 'POST',
      url,
      headers: { authorization: `Bearer ${token}` },
      payload,
    });
    expect(res.statusCode).toBe(403);
    expect(res.json()).toMatchObject({ error: 'agent_token_read_only' });
  });

  it('requires a signature when no credential is presented', async () => {
    const res = await app.inject({
      method: 'POST',
      url: '/v1/agent/sign-message',
      payload: { message: 'hello' },
    });
    expect(res.statusCode).toBe(401);
    expect(res.json()).toMatchObject({ error: 'agent_signature_required' });
  });

  it('refuses a signed request from a frozen agent', async () => {
    const res = await signedPost(agent, '/v1/agent/sign-message', { message: 'hello' });
    expect(res.statusCode).toBe(403);
    expect(res.json()).toMatchObject({ error: 'agent_frozen' });
  });

  describe('once the owner unfreezes the agent', () => {
    beforeAll(async () => {
      const did = encodeURIComponent(agent.did);
      expect((await ownerCall(owner, 'POST', `/v1/agents/${did}/unfreeze`)).statusCode).toBe(200);
      expect((await ownerCall(owner, 'PUT', `/v1/agents/${did}/policy`, { mode: 'full' })).statusCode).toBe(200);
    }, 60_000);

    it('signs a message for a correctly signed request and refuses its replay', async () => {
      const nonce = randomBytes(16);
      const message = 'consented message';
      const res = await signedPost(agent, '/v1/agent/sign-message', { message }, { nonce });
      expect(res.statusCode).toBe(200);
      const body = res.json() as { signature: `0x${string}`; address: `0x${string}` };
      expect(await verifyMessage({ address: body.address, message, signature: body.signature })).toBe(true);

      const replay = await signedPost(agent, '/v1/agent/sign-message', { message }, { nonce });
      expect(replay.statusCode).toBe(401);
      expect(replay.json()).toMatchObject({ error: 'agent_nonce_replayed' });
    });

    it('refuses an expired request', async () => {
      const res = await signedPost(
        agent,
        '/v1/agent/sign-message',
        { message: 'late' },
        { expiry: BigInt(Math.floor(Date.now() / 1000) - 1) },
      );
      expect(res.statusCode).toBe(401);
      expect(res.json()).toMatchObject({ error: 'agent_request_expired' });
    });

    it('refuses an expiry beyond the allowed window', async () => {
      const res = await signedPost(
        agent,
        '/v1/agent/sign-message',
        { message: 'early' },
        { expiry: BigInt(Math.floor(Date.now() / 1000) + 3_600) },
      );
      expect(res.statusCode).toBe(401);
      expect(res.json()).toMatchObject({ error: 'agent_request_expiry_too_far' });
    });

    it('refuses a body that differs from the signed one', async () => {
      const res = await signedPost(
        agent,
        '/v1/agent/sign-message',
        { message: 'tampered' },
        { signedBody: JSON.stringify({ message: 'original' }) },
      );
      expect(res.statusCode).toBe(401);
      expect(res.json()).toMatchObject({ error: 'agent_bad_signature' });
    });

    it('refuses a signature made by another key', async () => {
      const res = await signedPost(
        agent,
        '/v1/agent/sign-message',
        { message: 'other key' },
        { signer: newAgentKey('impostor') },
      );
      expect(res.statusCode).toBe(401);
      expect(res.json()).toMatchObject({ error: 'agent_bad_signature' });
    });

    it('refuses a key that is not registered', async () => {
      const res = await signedPost(newAgentKey('stranger'), '/v1/agent/sign-message', { message: 'who' });
      expect(res.statusCode).toBe(401);
      expect(res.json()).toMatchObject({ error: 'agent_unknown_key' });
    });

    it('refuses malformed signature headers', async () => {
      const body = JSON.stringify({ message: 'bad headers' });
      const headers = signedHeaders(agent, 'POST', '/v1/agent/sign-message', body);
      const res = await app.inject({
        method: 'POST',
        url: '/v1/agent/sign-message',
        headers: { ...headers, 'x-agent-nonce': 'zz' },
        payload: body,
      });
      expect(res.statusCode).toBe(401);
      expect(res.json()).toMatchObject({ error: 'agent_request_malformed' });
    });
  });
});
