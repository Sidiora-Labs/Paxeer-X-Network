import { createHash } from 'node:crypto';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import type { Hex } from 'viem';
import type { PoolClient } from 'pg';
import { env } from '../env.js';
import { encrypt, decrypt, loadMasterKey } from '../crypto.js';
import { query } from './pool.js';

/**
 * Wallet repository — owns the encrypted private-key lifecycle on our local
 * Postgres. Queries are strictly parameterised (no string interpolation).
 *
 * Plaintext private keys NEVER leave this module. Callers receive either:
 *   - a `SigningAccount` (for in-memory signing within one request), or
 *   - a `PublicWallet` (for read-only callers).
 *
 * The `SigningAccount` holds the plaintext key in memory only for the caller's
 * await chain; it is discarded when the request returns. There is no
 * persistent in-memory cache.
 */

export type WalletKind = 'standard' | 'funded' | 'agent';

/**
 * Deterministic synthetic user_id for an agent's dedicated wallet.
 *
 * `wallets.user_id` is a UUID with a UNIQUE(user_id, kind) index and NO
 * cross-DB foreign key (see migrations/001_init.sql). Agents are not Supabase
 * users, so we derive a stable UUID from the DID: sha256("…:<did>") truncated
 * to 16 bytes with the version/variant bits set. This gives every agent DID
 * exactly one kind='agent' wallet, reusing the full encrypt/sign/provision
 * machinery below with zero schema change — and it can never collide with a
 * real Supabase user UUID namespace.
 */
export function agentWalletUserId(did: string): string {
  const h = createHash('sha256').update(`paxeer-agent-wallet:${did}`).digest();
  const b = Buffer.from(h.subarray(0, 16));
  b[6] = (b[6]! & 0x0f) | 0x80; // version 8 (custom/name-based)
  b[8] = (b[8]! & 0x3f) | 0x80; // RFC 4122 variant
  const hex = b.toString('hex');
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20, 32)}`;
}

export interface WalletRow {
  id: string;
  user_id: string;
  address: `0x${string}`;
  encrypted_private_key: string;
  key_version: number;
  chain_id: number;
  kind: WalletKind;
  created_at: string;
  last_used_at: string | null;
  is_disabled: boolean;
  disabled_reason: string | null;
}

export interface PublicWallet {
  id: string;
  address: `0x${string}`;
  chain_id: number;
  kind: WalletKind;
  created_at: string;
  last_used_at: string | null;
}

function toPublic(row: WalletRow): PublicWallet {
  return {
    id: row.id,
    address: row.address,
    chain_id: row.chain_id,
    kind: row.kind,
    created_at: row.created_at,
    last_used_at: row.last_used_at,
  };
}

/**
 * Look up the wallet for a user. Returns null if none exists yet.
 *
 * `kind` defaults to 'standard'. Archived rows are never returned.
 */
export async function findWalletByUserId(
  userId: string,
  kind: WalletKind = 'standard',
): Promise<WalletRow | null> {
  const { rows } = await query<WalletRow>(
    `select id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
            created_at, last_used_at, is_disabled, disabled_reason
       from wallets
      where user_id = $1 and kind = $2 and archived_at is null
      limit 1`,
    [userId, kind],
  );
  return rows[0] ?? null;
}

export interface SigningWallet {
  row: WalletRow;
  migratedAt: string | null;
  attestorKeyId: string | null;
}

export async function loadWalletForSigning(
  client: PoolClient,
  userId: string,
  kind: WalletKind = 'standard',
): Promise<SigningWallet | null> {
  const { rows } = await client.query<WalletRow & { migrated_at: string | null; attestor_key_id: string | null }>(
    `select id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
            created_at, last_used_at, is_disabled, disabled_reason, migrated_at, attestor_key_id
       from wallets
      where user_id = $1 and kind = $2 and archived_at is null
      limit 1
      for share`,
    [userId, kind],
  );
  const r = rows[0];
  if (!r) return null;
  const { migrated_at, attestor_key_id, ...row } = r;
  return { row, migratedAt: migrated_at, attestorKeyId: attestor_key_id };
}

export async function findWalletByAddress(address: string): Promise<WalletRow | null> {
  const { rows } = await query<WalletRow>(
    `select id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
            created_at, last_used_at, is_disabled, disabled_reason
       from wallets
      where lower(address) = lower($1) and archived_at is null
      limit 1`,
    [address],
  );
  return rows[0] ?? null;
}

export type ArchivedWalletLookup =
  | { id: string }
  | { address: string }
  | { userId: string; kind: WalletKind };

export interface ArchivedWalletRefusal {
  status: 410;
  body: {
    error: 'wallet_archived';
    message: string;
    wallet_id: string;
    address: `0x${string}`;
  };
}

const UUID_TEXT_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const ADDRESS_TEXT_RE = /^0x[0-9a-fA-F]{40}$/;

export async function findArchivedWallet(
  lookup: ArchivedWalletLookup,
): Promise<{ id: string; address: `0x${string}`; archived_at: string } | null> {
  let id: string | null = null;
  let address: string | null = null;
  let userId: string | null = null;
  let kind: WalletKind | null = null;
  if ('id' in lookup) {
    if (!UUID_TEXT_RE.test(lookup.id)) return null;
    id = lookup.id;
  } else if ('address' in lookup) {
    if (!ADDRESS_TEXT_RE.test(lookup.address)) return null;
    address = lookup.address;
  } else {
    if (!UUID_TEXT_RE.test(lookup.userId)) return null;
    userId = lookup.userId;
    kind = lookup.kind;
  }
  const { rows } = await query<{ id: string; address: `0x${string}`; archived_at: string }>(
    `select id, address, archived_at
       from wallets
      where archived_at is not null
        and (id = $1::uuid
             or lower(address) = lower($2::text)
             or (user_id = $3::uuid and kind = $4::text))
      limit 1`,
    [id, address, userId, kind],
  );
  return rows[0] ?? null;
}

export async function archivedWalletGuard(
  ...lookups: ArchivedWalletLookup[]
): Promise<ArchivedWalletRefusal | null> {
  for (const lookup of lookups) {
    const archived = await findArchivedWallet(lookup);
    if (archived) {
      return new WalletArchivedError(archived.id, archived.address).refusal;
    }
  }
  return null;
}

export class WalletArchivedError extends Error {
  readonly refusal: ArchivedWalletRefusal;
  constructor(walletId: string, address: `0x${string}`) {
    super(`wallet ${walletId} is archived`);
    this.name = 'WalletArchivedError';
    this.refusal = {
      status: 410,
      body: {
        error: 'wallet_archived',
        message: 'this wallet is archived and no longer served',
        wallet_id: walletId,
        address,
      },
    };
  }
}

/**
 * Idempotent: return the existing wallet of the given kind for the user, or
 * create a fresh one. Returns BOTH the public wallet and the raw row. Refuses
 * with WalletArchivedError when the user's wallet of that kind is archived.
 */
export async function provisionWalletForUser(
  userId: string,
  kind: WalletKind = 'standard',
): Promise<{ wallet: PublicWallet; row: WalletRow }> {
  const existing = await findWalletByUserId(userId, kind);
  if (existing) {
    if (existing.is_disabled) {
      throw new Error(
        `wallet for user ${userId} is disabled: ${existing.disabled_reason ?? 'unknown reason'}`,
      );
    }
    return { wallet: toPublic(existing), row: existing };
  }
  const archived = await findArchivedWallet({ userId, kind });
  if (archived) throw new WalletArchivedError(archived.id, archived.address);

  // Generate a fresh EOA. `viem.generatePrivateKey` uses crypto.randomBytes(32).
  const privateKey = generatePrivateKey();
  const account = privateKeyToAccount(privateKey);

  // Encrypt the key under the master key. Plaintext drops out of scope here.
  const masterKey = loadMasterKey(env.WALLET_MASTER_KEY);
  const { ciphertext, version } = encrypt(
    Buffer.from(privateKey.slice(2), 'hex'),
    masterKey,
    env.WALLET_MASTER_KEY_VERSION,
  );

  try {
    const { rows } = await query<WalletRow>(
      `insert into wallets (user_id, address, encrypted_private_key, key_version, chain_id, kind)
       values ($1, $2, $3, $4, $5, $6)
       returning id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
                 created_at, last_used_at, is_disabled, disabled_reason`,
      [userId, account.address, ciphertext, version, env.HYPERPAXEER_CHAIN_ID, kind],
    );
    if (!rows[0]) throw new Error('db.provisionWalletForUser: insert returned no row');
    return { wallet: toPublic(rows[0]), row: rows[0] };
  } catch (err) {
    // 23505 = unique_violation. Another request inserted between our find +
    // insert — pick up that row instead.
    if (isUniqueViolation(err)) {
      const racer = await findWalletByUserId(userId, kind);
      if (racer) return { wallet: toPublic(racer), row: racer };
    }
    throw new Error(
      `db.provisionWalletForUser: ${err instanceof Error ? err.message : String(err)}`,
    );
  }
}

/**
 * Decrypts the user's private key and returns a signing-only interface. The
 * raw key never escapes this function's closure.
 */
export interface SigningAccount {
  address: `0x${string}`;
  signTransaction: ReturnType<typeof privateKeyToAccount>['signTransaction'];
  signMessage: ReturnType<typeof privateKeyToAccount>['signMessage'];
  signTypedData: ReturnType<typeof privateKeyToAccount>['signTypedData'];
}

export async function getSigningAccountForUser(
  userId: string,
  kind: WalletKind = 'standard',
): Promise<SigningAccount> {
  const wallet = await findWalletByUserId(userId, kind);
  if (!wallet) {
    throw new Error(`no ${kind} wallet provisioned for user ${userId}`);
  }
  if (wallet.is_disabled) {
    throw new Error(
      `wallet disabled for user ${userId}: ${wallet.disabled_reason ?? 'unknown reason'}`,
    );
  }

  return getSigningAccountForRow(wallet);
}

/**
 * Variant that takes an already-loaded WalletRow, for callers that have the
 * row in hand and want to skip a second lookup.
 */
export async function getSigningAccountForRow(wallet: WalletRow): Promise<SigningAccount> {
  const masterKey = loadMasterKey(env.WALLET_MASTER_KEY);
  const plaintext = decrypt(wallet.encrypted_private_key, masterKey);
  const privateKey = (`0x${plaintext.toString('hex')}`) as Hex;
  const account = privateKeyToAccount(privateKey);

  // Best-effort scrub — Node may hold copies in libuv / V8 internals but this
  // reduces residency window.
  plaintext.fill(0);

  return {
    address: account.address,
    signTransaction: account.signTransaction.bind(account),
    signMessage: account.signMessage.bind(account),
    signTypedData: account.signTypedData.bind(account),
  };
}

/** Record a signing event for audit + rate-limit purposes. */
export interface SignatureLogInput {
  user_id: string;
  wallet_id: string;
  address: `0x${string}`;
  kind: 'transaction' | 'message' | 'typed_data';
  to_address?: string | null;
  value_wei?: bigint | null;
  chain_id?: number | null;
  request_hash: string;
  tx_hash?: string | null;
  ip?: string | null;
  user_agent?: string | null;
  /** Set for agent-originated signatures so per-agent activity is queryable. */
  principal_did?: string | null;
}

export async function logSignature(input: SignatureLogInput): Promise<void> {
  try {
    await query(
      `insert into wallet_signatures
         (user_id, wallet_id, address, kind, to_address, value_wei, chain_id,
          request_hash, tx_hash, ip, user_agent, principal_did)
       values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
      [
        input.user_id,
        input.wallet_id,
        input.address,
        input.kind,
        input.to_address ?? null,
        input.value_wei != null ? input.value_wei.toString() : '0',
        input.chain_id ?? null,
        input.request_hash,
        input.tx_hash ?? null,
        input.ip ?? null,
        input.user_agent ?? null,
        input.principal_did ?? null,
      ],
    );
  } catch (err) {
    // Audit is non-fatal — better to complete the user-visible op and warn,
    // than to fail signing because the log table hiccupped.
    // eslint-disable-next-line no-console
    console.warn(
      `[wallets] logSignature failed (non-fatal): ${err instanceof Error ? err.message : String(err)}`,
    );
  }
}

function isUniqueViolation(err: unknown): boolean {
  return (
    typeof err === 'object' &&
    err !== null &&
    'code' in err &&
    (err as { code?: string }).code === '23505'
  );
}

export const _public = { toPublic };
