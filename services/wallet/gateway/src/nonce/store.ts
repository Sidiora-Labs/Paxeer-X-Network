import type { Pool, PoolClient } from 'pg';
import { keccak256, type Hex } from 'viem';
import { env } from '../env.js';
import { getPool } from '../db/pool.js';
import { BROADCAST_GRACE_MS } from '../chain.js';
import { TransactionRejectedError, sharedRpcPool, type TransactionLookup } from '../rpc/pool.js';

export type PendingCountReader = (address: `0x${string}`) => Promise<number>;
export type TransactionLookupReader = (hash: `0x${string}`, member: string | null) => Promise<TransactionLookup>;

/** Outstanding broadcasts at most this far above the chain floor are asked for by hash on every allocation. */
const RECONCILE_WINDOW = 64;

/**
 * A nonce is outstanding from its reservation until the chain floor (the highest latest count seen)
 * passes it, and every reservation records the hash of the signed transaction the gateway produced,
 * including one handed to the caller to broadcast. Every allocation drops outstanding nonces below
 * the floor and asks every pool member by hash for each one in the reconcile window that is older
 * than the broadcast grace period: known anywhere keeps it, unknown on every member releases it, and
 * a member that does not answer leaves it unresolved, which fails the allocation only when the next
 * nonce depends on it. A wallet's first allocation seeds from latest only.
 */
export interface NonceLease {
  readonly address: string;
  /** Reserve the lowest free nonce at or above the floor; it must be tracked or released before the lock ends. */
  next(): Promise<number>;
  /** The nonce next() would hand out, without reserving it. */
  peek(): Promise<number>;
  /** Free an outstanding nonce whose transaction the node never accepted or that was never signed. */
  release(nonce: number): Promise<void>;
  /** Record the hash of the signed transaction at a nonce and the node that accepted it, if any. */
  track(nonce: number, txHash: Hex, member: string | null): Promise<void>;
  /** Refuse a caller-chosen nonce below the latest count before anything is signed or broadcast. */
  checkClientNonce(nonce: number): Promise<void>;
  /** Refuse a caller-chosen nonce below the floor or already outstanding, then reserve it. */
  reserveClientNonce(nonce: number): Promise<void>;
}

/**
 * Broadcast a signed transaction at a nonce this lease holds. A node rejection frees the nonce; any
 * other outcome (accepted, nonce conflict, timeout) records the signed transaction's hash so the
 * next allocation resolves the nonce by that hash.
 */
export async function broadcastReserved(
  lease: NonceLease,
  nonce: number,
  raw: Hex,
  send: (raw: Hex) => Promise<{ hash: Hex; member: string | null }>,
): Promise<{ hash: Hex; error: null } | { hash: null; error: unknown }> {
  try {
    const { hash, member } = await send(raw);
    await lease.track(nonce, keccak256(raw), member);
    return { hash, error: null };
  } catch (error) {
    if (error instanceof TransactionRejectedError) await lease.release(nonce);
    else await lease.track(nonce, keccak256(raw), null);
    return { hash: null, error };
  }
}

export class NonceBelowLatestError extends Error {
  constructor(readonly nonce: number, readonly latest: number) {
    super(`nonce ${nonce} is below the latest transaction count ${latest}`);
    this.name = 'NonceBelowLatestError';
  }
}

export class NonceOutstandingError extends Error {
  constructor(readonly nonce: number) {
    super(`nonce ${nonce} is already outstanding for this wallet`);
    this.name = 'NonceOutstandingError';
  }
}

/** The next nonce depends on an outstanding one that pool members could not resolve; nothing is released. */
export class NonceUnresolvedError extends Error {
  constructor(readonly nonce: number, readonly members: string[]) {
    super(`nonce ${nonce} cannot be resolved: ${members.join(', ')} did not answer`);
    this.name = 'NonceUnresolvedError';
  }
}

export interface NonceStoreOptions {
  pool: Pool;
  chainId: number;
  pendingCount: PendingCountReader;
  lookupTransaction: TransactionLookupReader;
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
      const { rows } = await client.query<{ next_nonce: string }>(
        `select next_nonce::text as next_nonce
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
      const lease = new Lease(client, key, this.opts.pendingCount, this.opts.lookupTransaction);
      const result = await fn(lease);
      if (lease.untracked.size > 0) {
        throw new Error(`nonce store: reserved nonce ${[...lease.untracked].join(', ')} was neither tracked with a signed transaction hash nor released`);
      }
      await client.query('COMMIT');
      return result;
    } catch (err) {
      await client.query('ROLLBACK').catch(() => undefined);
      throw err;
    } finally {
      client.release();
    }
  }
}

class Lease implements NonceLease {
  readonly address: string;
  /** Nonces this lease reserved that carry no signed transaction hash yet. */
  readonly untracked = new Set<number>();

  constructor(
    private readonly client: PoolClient,
    address: string,
    private readonly latestCount: PendingCountReader,
    private readonly lookup: TransactionLookupReader,
  ) {
    this.address = address;
  }

  private async latest(): Promise<number> {
    const onChain = await this.latestCount(this.address as `0x${string}`);
    if (!Number.isSafeInteger(onChain) || onChain < 0) {
      throw new Error(`nonce store: chain answered an invalid transaction count ${onChain}`);
    }
    return onChain;
  }

  /** Drop what the chain passed, release broadcasts no member knows, return the floor and the lowest free nonce. */
  private async resolve(): Promise<{ floor: number; next: number }> {
    const latest = await this.latest();
    const { rows: seen } = await this.client.query<{ latest_seen: string }>(
      `update nonce_allocations set latest_seen = greatest(latest_seen, $2), updated_at = now()
        where address = $1 returning latest_seen::text as latest_seen`,
      [this.address, latest],
    );
    const floor = Number(seen[0]!.latest_seen);
    await this.client.query('delete from nonce_outstanding where address = $1 and nonce < $2', [this.address, floor]);
    const { rows: aged } = await this.client.query<{ nonce: string; tx_hash: Hex; member: string | null }>(
      `select nonce::text as nonce, tx_hash, member
         from nonce_outstanding
        where address = $1 and tx_hash is not null and nonce <= $2
          and created_at <= now() - make_interval(secs => $3::double precision / 1000)
        order by nonce_outstanding.nonce`,
      [this.address, floor + RECONCILE_WINDOW, BROADCAST_GRACE_MS],
    );
    const unresolved: Array<{ nonce: number; members: string[] }> = [];
    for (const row of aged) {
      const { known, unreachable } = await this.lookup(row.tx_hash, row.member);
      if (known) continue;
      if (unreachable.length > 0) unresolved.push({ nonce: Number(row.nonce), members: unreachable });
      else await this.client.query('delete from nonce_outstanding where address = $1 and nonce = $2', [this.address, row.nonce]);
    }
    const { rows } = await this.client.query<{ nonce: string }>(
      'select nonce::text as nonce from nonce_outstanding where address = $1 order by nonce_outstanding.nonce',
      [this.address],
    );
    let next = floor;
    for (const row of rows) {
      if (Number(row.nonce) !== next) break;
      next += 1;
    }
    if (!Number.isSafeInteger(next)) throw new Error(`nonce store: next nonce ${next} is not a safe integer`);
    const blocking = unresolved.find((u) => u.nonce < next);
    if (blocking) throw new NonceUnresolvedError(blocking.nonce, blocking.members);
    return { floor, next };
  }

  private async hold(nonce: number, txHash: string | null, member: string | null = null): Promise<void> {
    await this.client.query(
      `insert into nonce_outstanding (address, nonce, tx_hash, member) values ($1, $2, $3, $4)
       on conflict (address, nonce) do update set tx_hash = excluded.tx_hash, member = excluded.member, updated_at = now()`,
      [this.address, nonce, txHash, member],
    );
  }

  async peek(): Promise<number> {
    return (await this.resolve()).next;
  }

  async next(): Promise<number> {
    const { next } = await this.resolve();
    await this.hold(next, null);
    this.untracked.add(next);
    await this.client.query(
      `update nonce_allocations
          set next_nonce = greatest((select max(nonce) + 1 from nonce_outstanding where address = $1), latest_seen),
              needs_reconcile = false, reconciled_at = now(), updated_at = now()
        where address = $1`,
      [this.address],
    );
    return next;
  }

  async checkClientNonce(nonce: number): Promise<void> {
    const onChain = await this.latest();
    if (nonce < onChain) throw new NonceBelowLatestError(nonce, onChain);
  }

  async reserveClientNonce(nonce: number): Promise<void> {
    const { floor } = await this.resolve();
    if (nonce < floor) throw new NonceBelowLatestError(nonce, floor);
    const { rows } = await this.client.query(
      'select 1 from nonce_outstanding where address = $1 and nonce = $2',
      [this.address, nonce],
    );
    if (rows[0]) throw new NonceOutstandingError(nonce);
    await this.hold(nonce, null);
    this.untracked.add(nonce);
  }

  async track(nonce: number, txHash: Hex, member: string | null): Promise<void> {
    await this.hold(nonce, txHash.toLowerCase(), member);
    this.untracked.delete(nonce);
  }

  async release(nonce: number): Promise<void> {
    this.untracked.delete(nonce);
    await this.client.query('delete from nonce_outstanding where address = $1 and nonce = $2', [this.address, nonce]);
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
    lookupTransaction: (hash, member) => rpc.lookupTransaction(hash, member),
  });
  return shared;
}
