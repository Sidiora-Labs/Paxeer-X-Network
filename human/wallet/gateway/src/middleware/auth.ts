import type { FastifyRequest, FastifyReply } from 'fastify';
import { verifyAccessToken } from '../auth/jwt.js';

declare module 'fastify' {
  interface FastifyRequest {
    user?: {
      id: string;
      email?: string | null;
    };
  }
}

/**
 * Auth preHandler — extracts a Supabase access token from the Authorization
 * header, verifies it locally (no Supabase round-trip), and attaches the user
 * to the request.
 *
 * Usage:
 *   app.post('/v1/wallet/sign', { preHandler: requireAuth }, handler)
 */
export async function requireAuth(req: FastifyRequest, reply: FastifyReply): Promise<void> {
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

  try {
    const payload = await verifyAccessToken(token);
    req.user = { id: payload.sub, email: payload.email ?? null };
  } catch (err) {
    // Log the actual reason server-side so we can debug 401s without
    // exposing token internals to the client. Decode the header (not the
    // signature) so we can see the alg/kid the client is trying to use.
    const detail = err instanceof Error ? err.message : 'invalid token';
    let header: unknown = null;
    try {
      const part = token.split('.')[0];
      if (part) header = JSON.parse(Buffer.from(part, 'base64url').toString('utf8'));
    } catch {
      // Token isn't even shaped like a JWT — leave header null.
    }
    req.log.warn({ detail, header }, '[auth] token rejected');
    void reply.code(401).send({ error: 'unauthorized', detail });
  }
}
