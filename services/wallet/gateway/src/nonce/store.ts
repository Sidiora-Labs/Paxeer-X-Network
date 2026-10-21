import type { Pool, PoolClient } from 'pg';
import { env } from '../env.js';
import { getPool } from '../db/pool.js';
import { sharedRpcPool } from '../rpc/pool.js';

export type PendingCountReader = (address: `0x${string}`) => Promise<number>;

export interface NonceLease {
  readonly address: string;
  /** Reserve max(stored next nonce, latest count) and advance the stored next nonce past it. */
  next(): Promise<number>;
  /** Free a reserved nonce after the node definitively rejected the transaction carrying it. */
  release(nonce: number): Promise<void>;
  /** Refuse a caller-chosen nonce below the latest count before anything is signed or broadcast. */
  checkClientNonce(nonce: number): Promise<void>;
  /** Record a broadcast at a nonce: stored next becomes max(stored next, nonce + 1). */
  advance(nonce: number): Promise<void>;
}

export class NonceBelowLatestError extends Error {
  constructor(readonly nonce: number, readonly latest: number) {
    super(`nonce ${nonce} is below the latest transaction count ${latest}`);
    this.name = 'NonceBelowLatestError';
  }
}

export interface NonceStoreOptions {
  pool: Pool;
  chainId: number;
  pendingCount: PendingCountReader;
}

interface NonceRow {
  next_nonce: string;
  needs_reconcile: boolean;
}

export class NonceStore {
  private readonly opts: NonceStoreOptions;

  constructor(opts: NonceStoreOptions) {
    this.opts = opts;
  }

  async withLock<T>(address: string, fn: (lease: NonceLease) => Promise<T>, ownerActionId?: string): Promise<T> {
    const key = address.toLowerCase();
    const client = await this.opts.pool.connect();
    try {
      await client.query('BEGIN');
      await client.query(
        `insert into nonce_allocations (address, chain_id, next_nonce, needs_reconcile)
         values ($1, $2, 0, true)
         on conflict (address) do nothing`,
        [key, this.opts.chainId],
      );
      const { rows } = await client.query<NonceRow>(
        `select next_nonce::text as next_nonce, needs_reconcile
           from nonce_allocations
          where address = $1
          for update`,
        [key],
      );
      if (!rows[0]) throw new Error(`nonce store: row for ${key} vanished under lock`);
      const reservation = await client.query<{ action_id: string }>(
        'select action_id from custody_signing_reservations where address = $1 and chain_id = $2',
        [key, this.opts.chainId],
      );
      if (reservation.rows[0] && reservation.rows[0].action_id !== ownerActionId) {
        throw new Error('wallet nonce is reserved by an action awaiting custody authorization or broadcast');
      }
      const custody = await client.query<{ id:string }>(
        "select id from wallet_custody_submissions where address=$1 and chain_id=$2 and state='pending' limit 1",[key,this.opts.chainId]);
      if(custody.rows[0]) throw new Error('wallet nonce is reserved by an unresolved custody submission');
      const lease = new Lease(client, key, rows[0], this.opts.pendingCount);
      const result = await fn(lease);
      await client.query('COMMIT');
      return result;
    } catch (err) {
      await client.query('ROLLBACK').catch(() => undefined);
      throw err;
    } finally {
      client.release();
    }
  }

  async allocate(address: string): Promise<number> {
    return this.withLock(address, (lease) => lease.next());
  }
}

class Lease implements NonceLease {
  readonly address: string;
  private nextNonce: bigint;

  constructor(
    private readonly client: PoolClient,
    address: string,
    row: NonceRow,
    private readonly latestCount: PendingCountReader,
  ) {
    this.address = address;
    this.nextNonce = BigInt(row.next_nonce);
  }

  private async latest(): Promise<number> {
    const onChain = await this.latestCount(this.address as `0x${string}`);
    if (!Number.isSafeInteger(onChain) || onChain < 0) {
      throw new Error(`nonce store: chain answered an invalid transaction count ${onChain}`);
    }
    return onChain;
  }

  async next(): Promise<number> {
    const onChain = await this.latest();
    const allocated = this.nextNonce > BigInt(onChain) ? this.nextNonce : BigInt(onChain);
    this.nextNonce = allocated + 1n;
    await this.client.query(
      `update nonce_allocations
          set next_nonce = $2, needs_reconcile = false, reconciled_at = now(), updated_at = now()
        where address = $1`,
      [this.address, this.nextNonce.toString()],
    );
    return Number(allocated);
  }

  async checkClientNonce(nonce: number): Promise<void> {
    const onChain = await this.latest();
    if (nonce < onChain) throw new NonceBelowLatestError(nonce, onChain);
  }

  async advance(nonce: number): Promise<void> {
    const after = BigInt(nonce) + 1n;
    if (after <= this.nextNonce) return;
    this.nextNonce = after;
    await this.client.query(
      `update nonce_allocations set next_nonce = $2, updated_at = now() where address = $1`,
      [this.address, this.nextNonce.toString()],
    );
  }

  async release(nonce: number): Promise<void> {
    if (this.nextNonce !== BigInt(nonce) + 1n) {
      throw new Error(`nonce store: nonce ${nonce} is not the newest reservation for ${this.address}`);
    }
    this.nextNonce = BigInt(nonce);
    await this.client.query(
      `update nonce_allocations set next_nonce = $2, updated_at = now() where address = $1`,
      [this.address, this.nextNonce.toString()],
    );
  }
}

let shared: NonceStore | null = null;

export function sharedNonceStore(): NonceStore {
  if (shared) return shared;
  const rpc = sharedRpcPool();
  shared = new NonceStore({
    pool: getPool(),
    chainId: env.HYPERPAXEER_CHAIN_ID,
    pendingCount: (address) => rpc.getTransactionCount(address, 'latest'),
  });
  return shared;
}
