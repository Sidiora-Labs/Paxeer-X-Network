import type { FastifyInstance } from 'fastify';
import { createHash } from 'node:crypto';
import { z } from 'zod';
import { requireAuth } from '../middleware/auth.js';
import { parseDid, verifyEd25519 } from '../auth/did.js';
import { claimPrincipal, findPrincipal } from '../db/agents.js';
import { env } from '../env.js';

export const AGENT_CLAIM_DOMAIN = 'PXW:AGENT-CLAIM:v1';

const U64_MAX = (1n << 64n) - 1n;

export const ClaimBody = z
  .object({
    expires: z.string().regex(/^\d{1,20}$/, 'expires must be decimal unix seconds'),
    signature: z.string().regex(/^[0-9a-fA-F]{128}$/, 'signature must be 128 hex characters'),
  })
  .strict();

function lengthPrefixed(value: string): Buffer {
  const bytes = Buffer.from(value, 'utf8');
  const len = Buffer.alloc(4);
  len.writeUInt32BE(bytes.length);
  return Buffer.concat([len, bytes]);
}

export function agentClaimDigest(did: string, ownerUserId: string, expiry: bigint): Buffer {
  if (expiry < 0n || expiry > U64_MAX) throw new RangeError('claim expiry out of range');
  const exp = Buffer.alloc(8);
  exp.writeBigUInt64BE(expiry);
  return createHash('sha256')
    .update(Buffer.from(AGENT_CLAIM_DOMAIN, 'utf8'))
    .update(lengthPrefixed(did))
    .update(lengthPrefixed(ownerUserId))
    .update(exp)
    .digest();
}

export type ClaimOutcome =
  | { ok: true; did: string; owner_user_id: string }
  | {
      ok: false;
      status: 401 | 404 | 409;
      error:
        | 'not_found'
        | 'already_owned'
        | 'claim_expired'
        | 'claim_expiry_too_far'
        | 'bad_claim_signature';
      message: string;
    };

export async function claimAgent(args: {
  did: string;
  ownerUserId: string;
  expiry: bigint;
  signatureHex: string;
  nowSeconds?: number;
}): Promise<ClaimOutcome> {
  const principal = await findPrincipal(args.did);
  if (!principal) {
    return { ok: false, status: 404, error: 'not_found', message: 'no such agent' };
  }
  if (principal.owner_user_id) {
    return { ok: false, status: 409, error: 'already_owned', message: 'agent already has an owner' };
  }
  const now = BigInt(args.nowSeconds ?? Math.floor(Date.now() / 1000));
  if (args.expiry <= now) {
    return { ok: false, status: 401, error: 'claim_expired', message: 'claim signature has expired' };
  }
  if (args.expiry - now > BigInt(env.AGENT_REQUEST_MAX_TTL_SECONDS)) {
    return {
      ok: false,
      status: 401,
      error: 'claim_expiry_too_far',
      message: `claim expiry must be within ${env.AGENT_REQUEST_MAX_TTL_SECONDS} seconds`,
    };
  }
  const digest = agentClaimDigest(principal.did, args.ownerUserId, args.expiry);
  if (!verifyEd25519(principal.public_key, digest, args.signatureHex)) {
    return {
      ok: false,
      status: 401,
      error: 'bad_claim_signature',
      message: "the agent's counter-signature over the claim does not verify",
    };
  }
  const recorded = await claimPrincipal(principal.did, args.ownerUserId);
  if (!recorded) {
    return { ok: false, status: 409, error: 'already_owned', message: 'agent already has an owner' };
  }
  return { ok: true, did: principal.did, owner_user_id: args.ownerUserId };
}

export async function agentsRoutes(app: FastifyInstance): Promise<void> {
  app.post('/v1/agents/:did/claim', { preHandler: requireAuth }, async (req, reply) => {
    const { did } = req.params as { did: string };
    if (!parseDid(did)) return reply.code(400).send({ error: 'malformed_did' });
    const parsed = ClaimBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const expiry = BigInt(parsed.data.expires);
    if (expiry > U64_MAX) {
      return reply.code(400).send({ error: 'invalid_body', message: 'expires out of range' });
    }
    const outcome = await claimAgent({
      did,
      ownerUserId: req.user!.id,
      expiry,
      signatureHex: parsed.data.signature,
    });
    if (!outcome.ok) {
      return reply.code(outcome.status).send({ error: outcome.error, message: outcome.message });
    }
    return reply.send({ did: outcome.did, owner_user_id: outcome.owner_user_id });
  });
}
