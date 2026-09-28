import { createHash } from 'node:crypto';
import type { FastifyInstance, FastifyReply, FastifyRequest } from 'fastify';
import { env } from '../env.js';
import { query } from '../db/pool.js';
import { findPrincipal, type AgentPrincipalRow } from '../db/agents.js';
import { parseDid, verifyEd25519 } from '../auth/did.js';
import { agentLaneEnabled } from '../auth/agentToken.js';

declare module 'fastify' {
  interface FastifyRequest {
    rawBody?: Buffer;
  }
}

export const AGENT_REQUEST_DOMAIN = 'PXW:AGENT-REQUEST:v1';

export const AGENT_REQUEST_HEADERS = {
  key: 'x-agent-key',
  nonce: 'x-agent-nonce',
  expires: 'x-agent-expires',
  signature: 'x-agent-signature',
} as const;

const U32_MAX = 0xffff_ffff;
const U64_MAX = (1n << 64n) - 1n;

export interface AgentRequestDigestInput {
  method: string;
  keyId: string;
  nonce: Buffer;
  expiry: bigint;
  body: Buffer;
}

function sha256(...parts: Buffer[]): Buffer {
  const h = createHash('sha256');
  for (const part of parts) h.update(part);
  return h.digest();
}

function u32be(n: number): Buffer {
  const out = Buffer.alloc(4);
  out.writeUInt32BE(n);
  return out;
}

function u64be(n: bigint): Buffer {
  const out = Buffer.alloc(8);
  out.writeBigUInt64BE(n);
  return out;
}

export function agentRequestDigest(input: AgentRequestDigestInput): Buffer {
  const method = Buffer.from(input.method, 'utf8');
  const keyId = Buffer.from(input.keyId, 'utf8');
  if (method.length > U32_MAX || keyId.length > U32_MAX) {
    throw new RangeError('agent request field exceeds length limit');
  }
  if (input.nonce.length !== 16) {
    throw new RangeError('agent request nonce must be 16 bytes');
  }
  if (input.expiry < 0n || input.expiry > U64_MAX) {
    throw new RangeError('agent request expiry out of range');
  }
  return sha256(
    Buffer.from(AGENT_REQUEST_DOMAIN, 'utf8'),
    u32be(method.length),
    method,
    u32be(keyId.length),
    keyId,
    input.nonce,
    u64be(input.expiry),
    sha256(input.body),
  );
}

export function agentRequestMethod(req: Pick<FastifyRequest, 'method' | 'url'>): string {
  const path = req.url.split('?', 1)[0] ?? req.url;
  return `${req.method.toUpperCase()} ${path}`;
}

export type AgentRequestRefusalCode =
  | 'agent_signature_required'
  | 'agent_token_read_only'
  | 'agent_request_malformed'
  | 'agent_unknown_key'
  | 'agent_frozen'
  | 'agent_request_expired'
  | 'agent_request_expiry_too_far'
  | 'agent_bad_signature'
  | 'agent_nonce_replayed';

export interface AgentRequestRefusal {
  status: 401 | 403;
  code: AgentRequestRefusalCode;
  message: string;
}

export type AgentRequestVerdict =
  | { ok: true; principal: AgentPrincipalRow }
  | { ok: false; refusal: AgentRequestRefusal };

export interface AgentRequestInput {
  method: string;
  headers: Record<string, string | string[] | undefined>;
  body: Buffer;
  nowSeconds?: number;
}

function refuse(status: 401 | 403, code: AgentRequestRefusalCode, message: string): AgentRequestVerdict {
  return { ok: false, refusal: { status, code, message } };
}

function header(headers: AgentRequestInput['headers'], name: string): string | null {
  const v = headers[name];
  if (typeof v === 'string') return v.trim();
  return null;
}

export function hasAgentSignatureHeaders(headers: AgentRequestInput['headers']): boolean {
  return Object.values(AGENT_REQUEST_HEADERS).some((name) => headers[name] !== undefined);
}

export async function verifyAgentRequest(input: AgentRequestInput): Promise<AgentRequestVerdict> {
  const keyId = header(input.headers, AGENT_REQUEST_HEADERS.key);
  const nonceHex = header(input.headers, AGENT_REQUEST_HEADERS.nonce);
  const expiresText = header(input.headers, AGENT_REQUEST_HEADERS.expires);
  const signatureHex = header(input.headers, AGENT_REQUEST_HEADERS.signature);

  if (!keyId || !nonceHex || !expiresText || !signatureHex) {
    return refuse(401, 'agent_request_malformed', 'X-Agent-Key, X-Agent-Nonce, X-Agent-Expires and X-Agent-Signature are all required');
  }
  if (!parseDid(keyId)) {
    return refuse(401, 'agent_request_malformed', 'X-Agent-Key must be a registered agent DID');
  }
  if (!/^[0-9a-fA-F]{32}$/.test(nonceHex)) {
    return refuse(401, 'agent_request_malformed', 'X-Agent-Nonce must be 32 hex characters');
  }
  if (!/^\d{1,20}$/.test(expiresText) || BigInt(expiresText) > U64_MAX) {
    return refuse(401, 'agent_request_malformed', 'X-Agent-Expires must be decimal unix seconds');
  }
  if (!/^[0-9a-fA-F]{128}$/.test(signatureHex)) {
    return refuse(401, 'agent_request_malformed', 'X-Agent-Signature must be 128 hex characters');
  }

  const principal = await findPrincipal(keyId);
  if (!principal) {
    return refuse(401, 'agent_unknown_key', 'agent key is not registered');
  }
  if (principal.is_frozen) {
    return refuse(403, 'agent_frozen', 'agent is frozen by its owner');
  }

  const expiry = BigInt(expiresText);
  const now = BigInt(input.nowSeconds ?? Math.floor(Date.now() / 1000));
  if (expiry <= now) {
    return refuse(401, 'agent_request_expired', 'agent request has expired');
  }
  if (expiry - now > BigInt(env.AGENT_REQUEST_MAX_TTL_SECONDS)) {
    return refuse(
      401,
      'agent_request_expiry_too_far',
      `agent request expiry must be within ${env.AGENT_REQUEST_MAX_TTL_SECONDS} seconds`,
    );
  }

  const nonce = Buffer.from(nonceHex, 'hex');
  const digest = agentRequestDigest({ method: input.method, keyId, nonce, expiry, body: input.body });
  if (!verifyEd25519(principal.public_key, digest, signatureHex)) {
    return refuse(401, 'agent_bad_signature', 'agent request signature does not verify');
  }

  await query(`delete from agent_request_nonces where expires_at < now()`);
  const inserted = await query(
    `insert into agent_request_nonces (did, nonce, expires_at)
     values ($1, $2, to_timestamp($3::bigint))
     on conflict (did, nonce) do nothing`,
    [principal.did, nonceHex.toLowerCase(), expiry.toString()],
  );
  if ((inserted.rowCount ?? 0) === 0) {
    return refuse(401, 'agent_nonce_replayed', 'agent request nonce was already used');
  }

  return { ok: true, principal };
}

export async function requireSignedAgentRequest(req: FastifyRequest, reply: FastifyReply): Promise<void> {
  if (!agentLaneEnabled()) {
    void reply
      .code(503)
      .send({ error: 'agent_lane_disabled', message: 'agent lane not configured (AGENT_JWT_SECRET unset)' });
    return;
  }
  const headers = req.headers as AgentRequestInput['headers'];
  if (!hasAgentSignatureHeaders(headers)) {
    const auth = req.headers.authorization;
    if (typeof auth === 'string' && auth.startsWith('Bearer ')) {
      void reply.code(403).send({
        error: 'agent_token_read_only',
        message: 'the agent token authorises read routes only; sign this request with the agent key',
      });
      return;
    }
    void reply.code(401).send({
      error: 'agent_signature_required',
      message: 'this route requires an agent request signature',
    });
    return;
  }

  const verdict = await verifyAgentRequest({
    method: agentRequestMethod(req),
    headers,
    body: req.rawBody ?? Buffer.alloc(0),
  });
  if (!verdict.ok) {
    void reply.code(verdict.refusal.status).send({ error: verdict.refusal.code, message: verdict.refusal.message });
    return;
  }
  req.agent = {
    did: verdict.principal.did,
    ownerUserId: verdict.principal.owner_user_id,
    principal: verdict.principal,
  };
}

export function installRawBodyParser(app: FastifyInstance): void {
  const parseJson = app.getDefaultJsonParser('error', 'error');
  app.removeContentTypeParser('application/json');
  app.addContentTypeParser('application/json', { parseAs: 'buffer' }, (req, body, done) => {
    const raw = body as Buffer;
    req.rawBody = raw;
    parseJson(req, raw.toString('utf8'), done);
  });
}
