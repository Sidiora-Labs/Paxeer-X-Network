import type { FastifyInstance } from 'fastify';
import { randomBytes } from 'node:crypto';
import { env } from '../env.js';
import { ChallengeBody, VerifyBody } from '../schemas/agent.js';
import { challengeMessage, parseDid, verifyAgentSignature } from '../auth/did.js';
import { agentLaneEnabled, mintAgentToken } from '../auth/agentToken.js';
import {
  consumeChallenge,
  createChallenge,
  findPrincipal,
  getPolicy,
  purgeExpiredChallenges,
  upsertPrincipalOnVerify,
} from '../db/agents.js';
import { effectivePolicy } from '../policy/agent.js';
import { requireAgent } from '../middleware/principal.js';
import { agentWalletUserId, findWalletByUserId } from '../db/wallets.js';

/**
 * Agent DID auth lane.
 *
 *   POST /v1/agent/auth/challenge  → issue a single-use nonce
 *   POST /v1/agent/auth/verify     → ed25519-verify the signed nonce, mint token
 *   POST /v1/agent/auth/refresh    → re-mint a token for an already-auth'd agent
 *   GET  /v1/agent/whoami          → principal + wallet + effective policy
 *
 * All routes 503 cleanly when AGENT_JWT_SECRET is unset (lane disabled).
 */
export async function agentAuthRoutes(app: FastifyInstance): Promise<void> {
  const laneGuard = (reply: import('fastify').FastifyReply): boolean => {
    if (!agentLaneEnabled()) {
      void reply
        .code(503)
        .send({ error: 'agent_lane_disabled', message: 'agent lane not configured (AGENT_JWT_SECRET unset)' });
      return false;
    }
    return true;
  };

  app.post('/v1/agent/auth/challenge', async (req, reply) => {
    if (!laneGuard(reply)) return;
    const parsed = ChallengeBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const { did } = parsed.data;
    if (!parseDid(did)) {
      return reply.code(400).send({ error: 'malformed_did' });
    }

    const nonce = randomBytes(24).toString('base64url');
    try {
      await createChallenge(did, nonce, env.AGENT_CHALLENGE_TTL_SECONDS);
    } catch (err) {
      req.log.error({ err, did }, 'create challenge failed');
      return reply.code(500).send({ error: 'challenge_failed', detail: (err as Error).message });
    }
    // Opportunistic GC; non-blocking.
    void purgeExpiredChallenges();

    return reply.send({
      did,
      nonce,
      expires_in: env.AGENT_CHALLENGE_TTL_SECONDS,
      // The EXACT bytes the agent must ed25519-sign, as a UTF-8 string.
      message: challengeMessage(did, nonce).toString('utf8'),
    });
  });

  app.post('/v1/agent/auth/verify', async (req, reply) => {
    if (!laneGuard(reply)) return;
    const parsed = VerifyBody.safeParse(req.body);
    if (!parsed.success) {
      return reply.code(400).send({ error: 'invalid_body', issues: parsed.error.issues });
    }
    const { did, public_key, nonce, signature } = parsed.data;

    // 1. Crypto verify (pure; no DB write).
    const sig = verifyAgentSignature({
      did,
      publicKeyHex: public_key,
      nonce,
      signatureHex: signature,
    });
    if (!sig.ok || !sig.parsed || !sig.publicKeyHex) {
      return reply.code(401).send({ error: 'verification_failed', reason: sig.reason });
    }

    // 2. Atomically consume the nonce (single-use, unexpired, DID-bound).
    const consumed = await consumeChallenge(nonce, did);
    if (!consumed) {
      return reply
        .code(401)
        .send({ error: 'challenge_invalid', message: 'nonce unknown, expired, or already used' });
    }

    // 3. Upsert principal (+ default policy) and mint the read-scoped token.
    //    Ownership is recorded only through POST /v1/agents/:did/claim.
    let principal;
    try {
      principal = await upsertPrincipalOnVerify({
        did,
        label: sig.parsed.label,
        keyFingerprint: sig.parsed.keyFingerprint,
        publicKey: sig.publicKeyHex,
      });
    } catch (err) {
      req.log.error({ err, did }, 'principal upsert failed');
      return reply.code(500).send({ error: 'principal_upsert_failed', detail: (err as Error).message });
    }

    const minted = await mintAgentToken({ did, ownerUserId: principal.owner_user_id });
    return reply.send({
      ...minted,
      owner_user_id: principal.owner_user_id,
      is_frozen: principal.is_frozen,
      wallet_provisioned: Boolean(principal.wallet_id),
    });
  });

  app.post('/v1/agent/auth/refresh', { preHandler: requireAgent }, async (req, reply) => {
    const agent = req.agent!;
    const minted = await mintAgentToken({ did: agent.did, ownerUserId: agent.ownerUserId });
    return reply.send({
      ...minted,
      owner_user_id: agent.ownerUserId,
      is_frozen: agent.principal.is_frozen,
    });
  });

  app.get('/v1/agent/whoami', { preHandler: requireAgent }, async (req, reply) => {
    const agent = req.agent!;
    const [policy, wallet] = await Promise.all([
      getPolicy(agent.did),
      findWalletByUserId(agentWalletUserId(agent.did), 'agent'),
    ]);
    const eff = effectivePolicy(policy);
    return reply.send({
      did: agent.did,
      owner_user_id: agent.ownerUserId,
      label: agent.principal.label,
      is_frozen: agent.principal.is_frozen,
      created_at: agent.principal.created_at,
      last_seen_at: agent.principal.last_seen_at,
      wallet: wallet
        ? { id: wallet.id, address: wallet.address, chain_id: wallet.chain_id }
        : null,
      policy: {
        mode: eff.mode,
        max_tx_value_wei: eff.maxTxValueWei.toString(),
        max_daily_value_wei: eff.maxDailyValueWei.toString(),
        rate_limit_per_min: eff.rateLimitPerMin,
        max_approve_wei: eff.maxApproveWei.toString(),
        allow_native_transfer: eff.allowNativeTransfer,
        withdrawal_allowlist_only: eff.withdrawalAllowlistOnly,
        daily_reset_utc_hour: eff.dailyResetUtcHour,
      },
    });
  });
}
