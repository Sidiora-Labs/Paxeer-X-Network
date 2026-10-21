import { Pool } from 'pg';
import { env } from '../env.js';

/**
 * Read-only client for the LayerX sequencer Postgres (LAYER_X_DB_URI).
 *
 * We own the whole ecosystem (network + wallet + LayerX + agents), so instead
 * of scraping Deposit events we read the sequencer's ledger directly for
 * AUTHORITATIVE credit verification: a deposit is credited iff a `deposits` row
 * exists keyed on our on-chain tx hash. The connection is also used by the
 * mirror-sync worker to pull per-DID escrow/balance into the wallet DB.
 *
 * Strictly read-only: every query is a SELECT. Degrades gracefully — when
 * LAYER_X_DB_URI is unset, `layerxEnabled()` is false and callers fall back to
 * on-chain-only verification.
 */

let pool: Pool | null = null;

export function layerxEnabled(): boolean {
  return Boolean(env.LAYER_X_DB_URI);
}

function getLayerxPool(): Pool {
  if (!env.LAYER_X_DB_URI) {
    throw new Error('LAYER_X_DB_URI unset — LayerX DB access is disabled');
  }
  if (pool) return pool;
  pool = new Pool({
    connectionString: env.LAYER_X_DB_URI,
    max: 4, // small: verification + a periodic sync, not a hot path
    connectionTimeoutMillis: 5_000,
    idleTimeoutMillis: 30_000,
    statement_timeout: 8_000,
    ssl: false,
  });
  pool.on('error', (err) => {
    // eslint-disable-next-line no-console
    console.error('[layerx-db] idle client error:', err.message);
  });
  return pool;
}

export interface LayerxDeposit {
  did: string;
  evm_address: string | null;
  amount_usdx: string; // micro-USDX
  deposit_tx: string;
  created_at: string;
}

/**
 * Authoritative credit check: the LayerX `deposits` row for our on-chain tx.
 * Returns null until the sequencer's deposit watcher has observed + credited
 * the deposit (the poll target for the action's `credit_verification` phase).
 */
export async function getDepositByTx(txHash: string): Promise<LayerxDeposit | null> {
  const { rows } = await getLayerxPool().query<LayerxDeposit>(
    `select did, evm_address, amount_usdx::text, deposit_tx, created_at
       from deposits where lower(deposit_tx) = lower($1) limit 1`,
    [txHash],
  );
  return rows[0] ?? null;
}

export interface LayerxAccount {
  did: string;
  evm_address: string | null;
  balance_usdx: string;
  escrow_usdx: string;
  updated_at: string;
}

/**
 * Pull accounts touched since `sinceIso` for the mirror-sync worker. A null
 * cursor pulls everything (first sync). Bounded by `limit` per page.
 */
export async function listAccountsUpdatedSince(
  sinceIso: string | null,
  limit = 500,
): Promise<LayerxAccount[]> {
  const { rows } = await getLayerxPool().query<LayerxAccount>(
    `select did, evm_address, balance_usdx::text, escrow_usdx::text, updated_at
       from accounts
      where $1::timestamptz is null or updated_at > $1::timestamptz
      order by updated_at asc
      limit $2`,
    [sinceIso, limit],
  );
  return rows;
}

/** Resolve the DID a keccak256(did) claim maps to (deposit attribution). */
export async function resolveDidClaim(claim: string): Promise<string | null> {
  const { rows } = await getLayerxPool().query<{ did: string }>(
    `select did from did_claims where claim = lower($1) limit 1`,
    [claim.startsWith('0x') ? claim.slice(2) : claim],
  );
  return rows[0]?.did ?? null;
}

export async function closeLayerxPool(): Promise<void> {
  if (pool) {
    await pool.end().catch(() => undefined);
    pool = null;
  }
}
