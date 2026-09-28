import { createHash } from 'node:crypto';
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts';
import type { Hex } from 'viem';
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
 * `kind` defaults to 'standard' for backwards compat with the pre-funded-accounts
 * API. Funded-accounts paths pass kind='funded' to get the separate EOA.
 */
export async function findWalletByUserId(
  userId: string,
  kind: WalletKind = 'standard',
): Promise<WalletRow | null> {
  const { rows } = await query<WalletRow>(
    `select id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
            created_at, last_used_at, is_disabled, disabled_reason
       from wallets
      where user_id = $1 and kind = $2
      limit 1`,
    [userId, kind],
  );
  return rows[0] ?? null;
}

/** Look up a wallet by its on-chain address. Used by treasury / evaluator paths. */
export async function findWalletByAddress(address: string): Promise<WalletRow | null> {
  const { rows } = await query<WalletRow>(
    `select id, user_id, address, encrypted_private_key, key_version, chain_id, kind,
            created_at, last_used_at, is_disabled, disabled_reason
       from wallets
      where lower(address) = lower($1)
      limit 1`,
    [address],
  );
  return rows[0] ?? null;
}

/**
 * Idempotent: return the existing wallet of the given kind for the user, or
 * create a fresh one. Returns BOTH the public wallet and the raw row — the
 * funded-provision path needs `wallet_id` to insert into `funded_accounts`
 * inside the same transaction.
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
 * Variant that takes an already-loaded WalletRow. Used by the funded-account
 * flows that have the row in hand and want to skip a second lookup.
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
