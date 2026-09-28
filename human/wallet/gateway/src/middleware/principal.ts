import type { FastifyRequest, FastifyReply } from 'fastify';
import { verifyAgentToken, agentLaneEnabled } from '../auth/agentToken.js';
import { findPrincipal, type AgentPrincipalRow } from '../db/agents.js';

declare module 'fastify' {
  interface FastifyRequest {
    /** Set by requireAgent — the authenticated agent principal. */
    agent?: {
      did: string;
      ownerUserId: string | null;
      principal: AgentPrincipalRow;
    };
  }
}

/**
 * Agent auth preHandler.
 *
 * Verifies the Bearer agent_token (minted by /v1/agent/auth/verify), loads the
 * principal row (for freeze state, owner binding, wallet link), and attaches it
 * to req.agent. Distinct from middleware/auth.ts::requireAuth, which verifies a
 * human Supabase JWT into req.user.
 *
 * Returns 503 when the lane is unconfigured (AGENT_JWT_SECRET unset) so the
 * feature degrades gracefully instead of 500-ing.
 */
export async function requireAgent(req: FastifyRequest, reply: FastifyReply): Promise<void> {
  if (!agentLaneEnabled()) {
    void reply
      .code(503)
      .send({ error: 'agent_lane_disabled', message: 'agent lane not configured (AGENT_JWT_SECRET unset)' });
    return;
  }

  const header = req.headers.authorization;
  if (!header || typeof header !== 'string' || !header.startsWith('Bearer ')) {
    void reply.code(401).send({ error: 'missing or malformed Authorization header' });
    return;
  }
  const token = header.slice('Bearer '.length).trim();
  if (!token) {
    void reply.code(401).send({ error: 'empty bearer token' });
    return;
  }

  let did: string;
  try {
    const claims = await verifyAgentToken(token);
    did = claims.did;
  } catch (err) {
    const detail = err instanceof Error ? err.message : 'invalid token';
    req.log.warn({ detail }, '[agent] token rejected');
    void reply.code(401).send({ error: 'unauthorized', detail });
    return;
  }

  const principal = await findPrincipal(did);
  if (!principal) {
    void reply
      .code(401)
      .send({ error: 'unknown_principal', message: 'agent principal not found; re-authenticate' });
    return;
  }

  req.agent = { did: principal.did, ownerUserId: principal.owner_user_id, principal };
}

/**
 * True when the human in req.user owns the given principal. Used by the owner
 * control plane (which is gated by requireAuth → req.user) to authorise actions
 * on a specific agent DID.
 */
export function ownsPrincipal(req: FastifyRequest, principal: AgentPrincipalRow): boolean {
  return Boolean(req.user && principal.owner_user_id && req.user.id === principal.owner_user_id);
}
