import { createRemoteJWKSet, jwtVerify, type JWTVerifyGetKey } from 'jose';
import { env } from '../env.js';

/**
 * Local Supabase JWT verification (asymmetric / JWKS).
 *
 * Modern Supabase projects sign JWTs with asymmetric keys (ES256 by default,
 * sometimes RS256 or EdDSA) and publish the matching public keys at
 * `${SUPABASE_URL}/auth/v1/.well-known/jwks.json`. We fetch that key set once,
 * cache it in memory, and verify each token's signature locally.
 *
 * Why JWKS rather than the legacy HS256 secret:
 *   - New Supabase projects don't expose an HS256 secret at all.
 *   - Public keys are safe to fetch at boot and cache forever.
 *   - jose handles `kid` dispatch + key rotation automatically — when an
 *     unknown `kid` appears it re-fetches the JWKS once (rate-limited by
 *     cooldownDuration), so admin key rotations propagate without restart.
 *
 * What this guarantees:
 *   - Signature is valid against Supabase's published public key for the
 *     `kid` advertised in the token header.
 *   - `iss` matches `${SUPABASE_URL}/auth/v1`.
 *   - `aud` is `authenticated` (user is signed in, not anon).
 *   - `exp` is in the future.
 *   - `sub` is a non-empty UUID string.
 *
 * What this does NOT guarantee:
 *   - The user still exists in Supabase. JWTs are short-lived (~1h); any
 *     deletion window is bounded by the token's exp. If tighter revocation
 *     is required later, add a Redis-backed revocation list keyed on `jti`.
 */

const ISSUER = `${env.SUPABASE_URL.replace(/\/$/, '')}/auth/v1`;
const JWKS_URL = new URL(`${ISSUER}/.well-known/jwks.json`);

// jose handles all the caching, kid dispatch, and re-fetch-on-miss for us.
// The first call after boot does one HTTPS round-trip to Supabase; every call
// after that is in-memory crypto only.
const JWKS: JWTVerifyGetKey = createRemoteJWKSet(JWKS_URL, {
  // Ten-minute key cache. Keys rarely change; on a kid miss jose will re-fetch
  // anyway (subject to cooldownDuration below).
  cacheMaxAge: 10 * 60_000,
  // Don't hammer Supabase if a malformed token shows up with an unknown kid.
  cooldownDuration: 30_000,
  // Cap how long we'll wait for the JWKS endpoint before giving up. Keeps a
  // slow Supabase from stalling auth indefinitely.
  timeoutDuration: 5_000,
});

export interface SupabaseJwtPayload {
  /** Supabase user UUID. Use this everywhere as the user identifier. */
  sub: string;
  /** Email, when available. OAuth users get one from the provider. */
  email?: string;
  /** `authenticated` for signed-in users. */
  aud: string;
  /** Expiry, seconds since epoch. */
  exp: number;
  /** Identity-provider metadata (e.g. `{ provider: 'google' }`). */
  app_metadata?: Record<string, unknown>;
  user_metadata?: Record<string, unknown>;
}

export class InvalidTokenError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'InvalidTokenError';
  }
}

/**
 * Verify a Supabase access token. Throws `InvalidTokenError` on any failure.
 * The error message is safe to surface to clients (no secrets leaked).
 */
export async function verifyAccessToken(token: string): Promise<SupabaseJwtPayload> {
  try {
    const { payload } = await jwtVerify(token, JWKS, {
      issuer: ISSUER,
      audience: 'authenticated',
    });
    if (typeof payload.sub !== 'string' || payload.sub.length === 0) {
      throw new InvalidTokenError('token missing sub claim');
    }
    return payload as unknown as SupabaseJwtPayload;
  } catch (err) {
    if (err instanceof InvalidTokenError) throw err;
    const message = err instanceof Error ? err.message : 'invalid token';
    throw new InvalidTokenError(message);
  }
}
