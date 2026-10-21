import { createHash } from 'node:crypto';
import { query, withTransaction } from './pool.js';
import { parseAgentReauthorization, type AgentReauthorization } from '../attestor/client.js';

export async function retainAgentAuthorization(did: string, authorization: AgentReauthorization): Promise<string> {
  const canonical = JSON.stringify(authorization);
  const id = createHash('sha256').update(did).update('\0').update(canonical).digest('hex');
  await withTransaction(async (client) => {
    await client.query('select did from agent_principals where did = $1 for update', [did]);
    await client.query('delete from agent_signing_authorizations where did = $1 and expires_at <= now()', [did]);
    const existing = await client.query('select id from agent_signing_authorizations where id = $1 and did = $2', [id, did]);
    if (existing.rows.length) return;
    const count = await client.query<{ count: string }>('select count(*)::text as count from agent_signing_authorizations where did = $1', [did]);
    if (Number(count.rows[0]!.count) >= 256) throw new Error('agent authorization capacity exceeded');
    await client.query(`insert into agent_signing_authorizations (id, did, "authorization", expires_at)
      values ($1, $2, $3::jsonb, to_timestamp($4))`, [id, did, canonical, authorization.expiry]);
  });
  return id;
}

export async function loadAgentAuthorization(did: string, id: string): Promise<AgentReauthorization> {
  if (!/^[0-9a-f]{64}$/.test(id)) throw new Error('invalid agent authorization ID');
  const { rows } = await query<{ authorization: unknown }>(
    'select "authorization" from agent_signing_authorizations where id = $1 and did = $2 and expires_at > now()', [id, did],
  );
  if (rows.length !== 1) throw new Error('agent authorization unavailable or expired');
  return parseAgentReauthorization(rows[0]!.authorization);
}
