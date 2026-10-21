import { createPrivateKey, createPublicKey, sign, verify, type KeyObject } from 'node:crypto';
import { closeSync, constants, fstatSync, openSync, readFileSync } from 'node:fs';
import { isAbsolute } from 'node:path';
import { getPool } from '../db/pool.js';
import { env } from '../env.js';
import { serializeTransaction, type TransactionSerializableEIP1559 } from 'viem';

export interface WalletInventoryKey {
  key_id: string;
  epoch: number;
  curve: 'secp256k1' | 'ed25519';
  public_key: string;
  owner: string;
  operations: Array<'sign' | 'generate' | 'import' | 'refresh'>;
}
export interface WalletInventory {
  version: 1;
  iss: string;
  aud: 'wallet-custody-inventory';
  tenant: string;
  sequence: string;
  iat: number;
  exp: number;
  protocol: 'wallet';
  threshold: 3;
  members: Array<{ id: string; spki_sha256: string }>;
  keys: WalletInventoryKey[];
}

export class CustodyAuthorityError extends Error {
  readonly code = 'custody_authority_unavailable';
  readonly status = 503;
  readonly statusCode = 503;
  readonly replicationPending = true;
}

function producerKey(): KeyObject {
  const path = env.WALLET_IDENTITY_BINDING_PRIVATE_KEY_FILE;
  if (!path || !isAbsolute(path) || !env.WALLET_IDENTITY_BINDING_TENANT) throw new CustodyAuthorityError('wallet identity producer is not configured');
  let descriptor: number | undefined;
  try {
    descriptor = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
    const metadata = fstatSync(descriptor);
    if (!metadata.isFile() || metadata.nlink !== 1 || (metadata.mode & 0o777) !== 0o600
      || metadata.uid !== process.getuid?.() || metadata.size < 1 || metadata.size > 16_384) throw new Error('invalid producer file');
    const key = createPrivateKey(readFileSync(descriptor));
    if (key.asymmetricKeyType !== 'ec' || key.asymmetricKeyDetails?.namedCurve !== 'prime256v1') throw new Error('invalid producer key');
    return key;
  } catch { throw new CustodyAuthorityError('wallet identity producer is unavailable'); }
  finally { if (descriptor !== undefined) closeSync(descriptor); }
}

function encodeSigned(payload: unknown): string {
  const header = Buffer.from(JSON.stringify({ alg: 'ES256', typ: 'wallet-custody-authority+jwt' })).toString('base64url');
  const body = Buffer.from(JSON.stringify(payload)).toString('base64url');
  const input = `${header}.${body}`;
  if (Buffer.byteLength(input) > 1_900_000) throw new CustodyAuthorityError('custody snapshot exceeds bound');
  const signature = sign('sha256', Buffer.from(input, 'ascii'), { key: producerKey(), dsaEncoding: 'ieee-p1363' });
  return `${input}.${signature.toString('base64url')}`;
}

let inventoryHighWater = 0n;
let inventoryTokenAtHighWater: string | null = null;
export function loadWalletInventory(): WalletInventory {
  try {
    const file = env.WALLET_CUSTODY_INVENTORY_FILE;
    const pub = env.WALLET_CUSTODY_INVENTORY_PUBLIC_KEY_FILE;
    if (!file || !pub || !env.WALLET_IDENTITY_BINDING_TENANT) throw new Error('missing inventory authority');
    const token = readFileSync(file, 'utf8').trim();
    if (Buffer.byteLength(token) > 2_097_152) throw new Error('inventory exceeds bound');
    const parts = token.split('.');
    if (parts.length !== 3 || parts.some((v) => !/^[A-Za-z0-9_-]+$/.test(v))) throw new Error('invalid inventory');
    const header = JSON.parse(Buffer.from(parts[0]!, 'base64url').toString('utf8')) as Record<string, unknown>;
    if (Object.keys(header).sort().join(',') !== 'alg,typ' || header.alg !== 'ES256' || header.typ !== 'wallet-custody-inventory+jwt') throw new Error('invalid inventory signature type');
    const publicKey = createPublicKey(readFileSync(pub));
    if (publicKey.asymmetricKeyType !== 'ec' || publicKey.asymmetricKeyDetails?.namedCurve !== 'prime256v1') throw new Error('invalid inventory authority');
    const sig = Buffer.from(parts[2]!, 'base64url');
    if (sig.length !== 64 || !verify('sha256', Buffer.from(`${parts[0]}.${parts[1]}`, 'ascii'), { key: publicKey, dsaEncoding: 'ieee-p1363' }, sig)) throw new Error('invalid inventory signature');
    const inventory = JSON.parse(Buffer.from(parts[1]!, 'base64url').toString('utf8')) as WalletInventory;
    const now = Math.floor(Date.now() / 1000);
    if (inventory.version !== 1 || inventory.iss !== `${env.SUPABASE_URL.replace(/\/$/, '')}/auth/v1`
      || inventory.tenant !== env.WALLET_IDENTITY_BINDING_TENANT || inventory.aud !== 'wallet-custody-inventory'
      || inventory.protocol !== 'wallet' || inventory.threshold !== 3 || typeof inventory.sequence !== 'string' || !/^[1-9][0-9]{0,19}$/.test(inventory.sequence)
      || BigInt(inventory.sequence) > 18_446_744_073_709_551_615n || !Number.isSafeInteger(inventory.iat) || !Number.isSafeInteger(inventory.exp)
      || inventory.iat > now || inventory.exp <= now || inventory.exp <= inventory.iat || inventory.exp - inventory.iat > 2_592_000
      || !Array.isArray(inventory.members) || inventory.members.length !== 5 || !Array.isArray(inventory.keys) || inventory.keys.length > 65_536) throw new Error('inventory scope or lifetime invalid');
    const ids = new Set<string>(); const pins = new Set<string>(); const keys = new Set<string>();
    for (const member of inventory.members) {
      if (!member.id || typeof member.id !== 'string' || ids.has(member.id) || !/^[0-9a-f]{64}$/.test(member.spki_sha256) || pins.has(member.spki_sha256)) throw new Error('inventory members invalid');
      ids.add(member.id); pins.add(member.spki_sha256);
    }
    for (const entry of inventory.keys) {
      if (!entry.key_id || keys.has(entry.key_id) || !entry.owner || !Number.isSafeInteger(entry.epoch) || entry.epoch < 0
        || !['secp256k1', 'ed25519'].includes(entry.curve) || !Array.isArray(entry.operations) || !entry.operations.length
        || new Set(entry.operations).size !== entry.operations.length || entry.operations.some((op) => !['sign', 'generate', 'import', 'refresh'].includes(op))) throw new Error('inventory key invalid');
      if (entry.public_key === '') {
        if (entry.epoch !== 0 || entry.operations.length !== 1 || entry.operations[0] !== 'generate') throw new Error('inventory generation admission invalid');
      } else if (!(entry.curve === 'ed25519' ? /^[0-9a-f]{64}$/ : /^04[0-9a-f]{128}$/).test(entry.public_key)) throw new Error('inventory public key invalid');
      keys.add(entry.key_id);
    }
    const sequence = BigInt(inventory.sequence);
    if (sequence < inventoryHighWater || (sequence === inventoryHighWater && inventoryTokenAtHighWater !== token)) throw new Error('inventory rollback');
    inventoryHighWater = sequence; inventoryTokenAtHighWater = token;
    return inventory;
  } catch { throw new CustodyAuthorityError('approved wallet custody inventory is missing, stale or inconsistent'); }
}

export function requireInventoryKey(keyId: string, operation: WalletInventoryKey['operations'][number], owner?: string): WalletInventoryKey {
  const entry = loadWalletInventory().keys.find((key) => key.key_id === keyId);
  if (!entry || !entry.operations.includes(operation) || (owner !== undefined && entry.owner !== owner)) throw new CustodyAuthorityError(`owner-approved wallet inventory does not admit ${operation} for ${keyId}`);
  return entry;
}

export interface CustodyAuthorityPublisher {
  publishAuthority(token: string, sequence: string, required: 3 | 5): Promise<void>;
}

export async function publishCustodyAuthority(target?: CustodyAuthorityPublisher, required: 3 | 5 = 5): Promise<string> {
  const inventory = loadWalletInventory();
  if (env.ATTESTOR_ENDPOINTS.length !== 5 || env.ATTESTOR_QUORUM !== 3) throw new CustodyAuthorityError('wallet custody requires the approved five-member, threshold-three inventory');
  if (!target) {
    const { defaultWalletAttestors } = await import('../db/wallets.js');
    const configured = defaultWalletAttestors();
    if (!configured) throw new CustodyAuthorityError('wallet attestors are unavailable');
    target = configured;
  }
  const client = await getPool().connect();
  let locked = false;
  let transaction = false;
  try {
    await client.query(`select pg_advisory_lock(1380012884, 13)`); locked = true;
    await client.query('begin isolation level repeatable read'); transaction = true;
    const prior = await client.query<{ sequence: string; inventory_sequence: string }>(`select sequence::text, inventory_sequence::text from wallet_custody_authority where singleton = true for update`);
    if (!prior.rows[0]) throw new CustodyAuthorityError('custody authority durable state is unavailable');
    if (BigInt(inventory.sequence) < BigInt(prior.rows[0].inventory_sequence)) throw new CustodyAuthorityError('custody inventory rollback');
    const sequence = (BigInt(prior.rows[0].sequence) + 1n).toString();
    const { rows } = await client.query<{ principal: Record<string, unknown> }>(`
      select jsonb_build_object(
        'did', a.did, 'public_key', lower(regexp_replace(a.public_key, '^0x', '')),
        'owner_subject', coalesce(a.owner_user_id::text, ''),
        'frozen', a.is_frozen or coalesce(w.is_disabled, false) or w.archived_at is not null,
        'key_ids', to_jsonb(array_remove(array[w.attestor_key_id, w.layerx_key_id], null)),
        'policy', jsonb_build_object('mode', p.mode,
          'max_tx_value_wei', coalesce(p.max_tx_value_wei::text, $1),
          'max_daily_value_wei', coalesce(p.max_daily_value_wei::text, $2),
          'rate_limit_per_min', coalesce(p.rate_limit_per_min, $3::int),
          'max_approve_wei', coalesce(p.max_approve_wei::text, $4),
          'allow_native_transfer', p.allow_native_transfer, 'withdrawal_allowlist_only', p.withdrawal_allowlist_only,
          'daily_reset_utc_hour', p.daily_reset_utc_hour),
        'rules', coalesce((select jsonb_agg(jsonb_build_object('effect', r.effect, 'subject', r.subject,
          'value', lower(r.value), 'max_value_wei', r.max_value_wei::text) order by r.id)
          from agent_policy_rules r where r.did = a.did), '[]'::jsonb),
        'budgets', coalesce((select jsonb_agg(jsonb_build_object('id', b.id::text,
          'target_contract', b.target_contract, 'token', b.token, 'cap_wei', b.cap_wei::text,
          'spent_wei', b.spent_wei::text, 'expires_at', floor(extract(epoch from b.expires_at))::bigint) order by b.id)
          from agent_budgets b where b.did = a.did and b.active and b.expires_at > now()), '[]'::jsonb)
      ) as principal from agent_principals a left join wallets w on w.id = a.wallet_id and w.kind = 'agent'
      join agent_policies p on p.did = a.did
      order by a.did`, [env.AGENT_DEFAULT_MAX_TX_VALUE_WEI.toString(), env.AGENT_DEFAULT_MAX_DAILY_VALUE_WEI.toString(), env.AGENT_DEFAULT_RATE_LIMIT_PER_MINUTE, env.AGENT_DEFAULT_MAX_APPROVE_WEI.toString()]);
    const legacy = await client.query<{ did: string; value: string; recent: number; window: string }>(`
      select a.did, coalesce(sum(s.value_wei) filter (where s.kind = 'transaction' and s.created_at >= boundary.start), 0)::text as value,
        count(s.id) filter (where s.created_at >= now() - interval '60 seconds')::int as recent,
        extract(epoch from boundary.start)::bigint::text as window
      from agent_principals a join agent_policies p on p.did = a.did
      left join wallets w on w.id = a.wallet_id
      cross join lateral (select (date_trunc('day', now() at time zone 'UTC') + p.daily_reset_utc_hour * interval '1 hour') at time zone 'UTC' as reset) clock
      cross join lateral (select clock.reset - case when clock.reset > now() then interval '1 day' else interval '0 days' end as start) boundary
      left join wallet_signatures s on s.principal_did = a.did and s.created_at >= now() - interval '24 hours'
        and s.created_at < coalesce(w.migrated_at, 'infinity'::timestamptz)
      group by a.did, boundary.start`);
    for (const row of rows) {
      const history = legacy.rows.find((entry) => entry.did === row.principal.did);
      if (!history) throw new CustodyAuthorityError('agent policy history is unavailable');
      row.principal.legacy_spent = history.value;
      row.principal.legacy_window = Number(history.window);
      row.principal.legacy_recent_requests = history.recent;
    }
    const bound = await client.query<{ did: string; id: string; budget_id: string; value: string; transaction: string }>(`
      select r.did, r.id::text, r.budget_id::text, r.value_wei::text as value, r.unsigned_transaction as transaction
        from custody_budget_reservations r join agent_budgets b on b.id = r.budget_id and b.did = r.did
        where b.active and b.expires_at > now() order by r.id`);
    const actions = await client.query<{ did: string; id: string; budget_id: string; value: string; execution: Record<string, { draft?: Record<string, unknown> }> | null }>(`
      select a.did, a.id, a.reserved_budget_id as budget_id, a.reserved_value_wei::text as value,
        a.request->'_execution' as execution from agent_actions a
        join agent_budgets b on b.id::text = a.reserved_budget_id and b.did = a.did
        where b.active and b.expires_at > now() and a.terminal_at is null and a.reserved_value_wei > 0 order by a.id`);
    for (const action of actions.rows) {
      const draft = action.execution?.call?.draft;
      if (!draft || String(draft.value) !== action.value) continue;
      const tx: TransactionSerializableEIP1559 = { type: 'eip1559', chainId: Number(draft.chainId),
        to: String(draft.to) as `0x${string}`, data: String(draft.data) as `0x${string}`,
        value: BigInt(String(draft.value)), nonce: Number(draft.nonce), gas: BigInt(String(draft.gas)),
        maxFeePerGas: BigInt(String(draft.maxFeePerGas)), maxPriorityFeePerGas: BigInt(String(draft.maxPriorityFeePerGas)) };
      bound.rows.push({ did: action.did, id: `action:${action.id}:call`, budget_id: action.budget_id,
        value: action.value, transaction: serializeTransaction(tx, { r: '0x0', s: '0x0', yParity: 0 }) });
    }
    for (const row of rows) row.principal.reservations = bound.rows.filter((entry) => entry.did === row.principal.did)
      .map(({ id, budget_id, value, transaction }) => ({ id, budget_id, value, transaction }));
    const now = Math.floor(Date.now() / 1000);
    const token = encodeSigned({ version: 1, iss: `${env.SUPABASE_URL.replace(/\/$/, '')}/auth/v1`,
      aud: 'wallet-custody-authority', tenant: env.WALLET_IDENTITY_BINDING_TENANT,
      sequence, iat: now, exp: now + 60, principals: rows.map((row) => row.principal) });
    await client.query(`update wallet_custody_authority set sequence = $1, inventory_sequence = $2,
      snapshot = $3, published_at = null, acknowledged_participants = 0, updated_at = now() where singleton = true`, [sequence, inventory.sequence, token]);
    await client.query('commit'); transaction = false;
    await target.publishAuthority(token, sequence, required);
    await client.query(`update wallet_custody_authority set published_at = now(), acknowledged_participants = $2 where singleton = true and sequence = $1`, [sequence, required]);
    return sequence;
  } catch (error) {
    if (transaction) await client.query('rollback').catch(() => undefined);
    if (error instanceof CustodyAuthorityError) throw error;
    throw new CustodyAuthorityError(`custody authority snapshot was not durably accepted by ${required} approved participants`);
  } finally {
    try {
      if (locked) await client.query(`select pg_advisory_unlock(1380012884, 13)`);
    } catch (error) { client.release(true); throw error; }
    client.release();
  }
}
