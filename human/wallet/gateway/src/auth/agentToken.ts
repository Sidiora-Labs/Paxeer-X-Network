import { SignJWT, jwtVerify } from 'jose';
import { env } from '../env.js';

/**
 * Agent access token — minted by THIS API after a successful DID verify and
 * verified by THIS API on every agent request. Because we are both issuer and
 * verifier, a symmetric HS256 secret (AGENT_JWT_SECRET) is sufficient and keeps
 * the lane self-contained (no JWKS, no external IdP).
 *
 * Distinct from the human Supabase JWT path (auth/jwt.ts): that is asymmetric
 * (JWKS) and issued by Supabase. The principal middleware accepts either and
 * tags req.principal.type accordingly.
 */

const ISSUER = 'paxeer-wallet-api';
const AUDIENCE = 'paxeer-agent';

/** True when the agent lane is configured (secret present). */
export function agentLaneEnabled(): boolean {
  return Boolean(env.AGENT_JWT_SECRET);
}

function secret(): Uint8Array {
  if (!env.AGENT_JWT_SECRET) {
    throw new Error('AGENT_JWT_SECRET unset — agent lane disabled');
  }
  return new TextEncoder().encode(env.AGENT_JWT_SECRET);
}

export interface AgentTokenClaims {
  /** matrix://user/<did> — stable principal URI. */
  sub: string;
  /** The agent's DID. */
  did: string;
  /** Owner Supabase user id, when the DID is bound to one; else null. */
  owner: string | null;
}

export interface MintedAgentToken {
  token: string;
  token_type: 'Bearer';
  expires_in: number;
  did: string;
}

/** Mint a short-lived agent_token. Throws if the lane is disabled. */
export async function mintAgentToken(args: {
  did: string;
  ownerUserId: string | null;
}): Promise<MintedAgentToken> {
  const ttl = env.AGENT_TOKEN_TTL_SECONDS;
  const token = await new SignJWT({ did: args.did, owner: args.ownerUserId })
    .setProtectedHeader({ alg: 'HS256', typ: 'JWT' })
    .setSubject(`matrix://user/${args.did}`)
    .setIssuer(ISSUER)
    .setAudience(AUDIENCE)
    .setIssuedAt()
    .setExpirationTime(`${ttl}s`)
    .sign(secret());
  return { token, token_type: 'Bearer', expires_in: ttl, did: args.did };
}

export class InvalidAgentTokenError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'InvalidAgentTokenError';
  }
}

/** Verify an agent_token. Throws InvalidAgentTokenError on any failure. */
export async function verifyAgentToken(token: string): Promise<AgentTokenClaims> {
  try {
    const { payload } = await jwtVerify(token, secret(), {
      issuer: ISSUER,
      audience: AUDIENCE,
    });
    const did = typeof payload.did === 'string' ? payload.did : '';
    if (!did) throw new InvalidAgentTokenError('token missing did claim');
    const owner = typeof payload.owner === 'string' ? payload.owner : null;
    return { sub: String(payload.sub ?? ''), did, owner };
  } catch (err) {
    if (err instanceof InvalidAgentTokenError) throw err;
    throw new InvalidAgentTokenError(err instanceof Error ? err.message : 'invalid agent token');
  }
}
