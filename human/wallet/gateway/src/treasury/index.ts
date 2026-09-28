import {
  createPublicClient,
  createWalletClient,
  encodeFunctionData,
  http,
  type Hex,
  type PublicClient,
  type WalletClient,
} from 'viem';
import { privateKeyToAccount } from 'viem/accounts';
import { hyperPaxeer } from '../chain.js';
import { env } from '../env.js';

/**
 * Treasury — a single hot wallet that:
 *
 *   1. Funds new funded-account wallets with USDL + PAX on provision.
 *   2. Tops up funded wallets with PAX when they fall below threshold.
 *
 * It is NOT a wallet row in the `wallets` table. The private key lives in
 * process memory only (FUNDED_TREASURY_PRIVATE_KEY env). This keeps treasury
 * key material out of our user-data Postgres and makes rotation a
 * simple env update + restart, rather than a schema migration.
 *
 * Concurrency: every treasury send routes through `withWalletLock(TREASURY)`
 * so two parallel `/v1/funded/provision` calls don't collide on the same
 * nonce. See util/walletLock.ts.
 */

// -----------------------------------------------------------------------------
// Singleton bootstrap
// -----------------------------------------------------------------------------

interface TreasurySingleton {
  address: `0x${string}`;
  walletClient: WalletClient;
  publicClient: PublicClient;
}

let cached: TreasurySingleton | null = null;

/**
 * Lazy-initialise the treasury client. Throws if FUNDED_TREASURY_PRIVATE_KEY
 * is missing — funded-account routes that depend on this surface a 503
 * rather than crashing the worker.
 */
export function getTreasury(): TreasurySingleton {
  if (cached) return cached;
  if (!env.FUNDED_TREASURY_PRIVATE_KEY) {
    throw new TreasuryUnavailableError(
      'FUNDED_TREASURY_PRIVATE_KEY not set — funded-account features are disabled',
    );
  }
  const account = privateKeyToAccount(env.FUNDED_TREASURY_PRIVATE_KEY as Hex);
  const walletClient = createWalletClient({
    account,
    chain: hyperPaxeer,
    transport: http(env.HYPERPAXEER_RPC_URL),
  });
  const publicClient = createPublicClient({
    chain: hyperPaxeer,
    transport: http(env.HYPERPAXEER_RPC_URL),
  });
  cached = { address: account.address, walletClient, publicClient };
  return cached;
}

/** Stable lock key for `withWalletLock` so all treasury sends serialise. */
export const TREASURY_LOCK_KEY = '__treasury__';

export class TreasuryUnavailableError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'TreasuryUnavailableError';
  }
}

// -----------------------------------------------------------------------------
// On-chain helpers
// -----------------------------------------------------------------------------

const ERC20_TRANSFER_ABI = [
  {
    type: 'function',
    name: 'transfer',
    stateMutability: 'nonpayable',
    inputs: [
      { name: 'to', type: 'address' },
      { name: 'amount', type: 'uint256' },
    ],
    outputs: [{ name: '', type: 'bool' }],
  },
] as const;

const ERC20_BALANCE_OF_ABI = [
  {
    type: 'function',
    name: 'balanceOf',
    stateMutability: 'view',
    inputs: [{ name: 'owner', type: 'address' }],
    outputs: [{ name: '', type: 'uint256' }],
  },
] as const;

/**
 * Send an ERC-20 transfer from the treasury. Returns the broadcasted tx hash.
 * Caller is responsible for serialising this through `withWalletLock`.
 */
export async function treasuryErc20Transfer(args: {
  token: `0x${string}`;
  to: `0x${string}`;
  amount: bigint;
}): Promise<Hex> {
  const t = getTreasury();
  const data = encodeFunctionData({
    abi: ERC20_TRANSFER_ABI,
    functionName: 'transfer',
    args: [args.to, args.amount],
  });
  // sendTransaction will fetch nonce + gas under the hood. Wallet-lock prevents
  // two parallel calls colliding on the same nonce.
  return t.walletClient.sendTransaction({
    account: t.walletClient.account!,
    chain: hyperPaxeer,
    to: args.token,
    data,
    value: 0n,
  });
}

/**
 * Send native PAX from the treasury. Returns the broadcasted tx hash.
 * Caller is responsible for serialising this through `withWalletLock`.
 */
export async function treasuryNativeTransfer(args: {
  to: `0x${string}`;
  amount: bigint;
}): Promise<Hex> {
  const t = getTreasury();
  return t.walletClient.sendTransaction({
    account: t.walletClient.account!,
    chain: hyperPaxeer,
    to: args.to,
    value: args.amount,
  });
}

// -----------------------------------------------------------------------------
// Balance reads — quorum-protected against geo-distributed RPC lag
// -----------------------------------------------------------------------------
//
// HYPERPAXEER_RPC_URL is a global load balancer fronting ~100 validator-aligned
// nodes. Cross-region nodes (Sydney, Singapore) can trail a US-East commit by
// hundreds of ms. A single `readContract` / `getBalance` lands on a random
// node — sometimes one that hasn't yet propagated the latest block.
//
// For a wallet that just received a USDL transfer:
//   - caught-up nodes return the new balance (e.g. 25_000_000_000)
//   - behind  nodes return the pre-transfer balance (e.g. 0)
//   - NO node ever returns a HIGHER-than-real balance for an inbound transfer
//
// So `MAX(N parallel reads)` deterministically converges on the freshest view
// visible *somewhere* in the cluster. It cannot over-report. The same holds
// for native balance reads after a `treasuryNativeTransfer`.
//
// All balance reads in this module go through `readWithQuorum`. Tier sizing
// is `env.FUNDED_BALANCE_READ_QUORUM` (default 3).

/**
 * Fire N copies of `read()` in parallel against the load-balanced RPC; return
 * MAX across results. Failures count as 0n and are absorbed — as long as one
 * sample succeeds we have a valid lower-bound view. If ALL samples fail, we
 * rethrow the first error so the caller can surface it.
 */
async function readWithQuorum(
  read: () => Promise<bigint>,
  samples = env.FUNDED_BALANCE_READ_QUORUM,
): Promise<bigint> {
  const n = Math.max(1, samples);
  const results = await Promise.allSettled(
    Array.from({ length: n }, () => read()),
  );
  let max = -1n;
  let firstErr: unknown = null;
  for (const r of results) {
    if (r.status === 'fulfilled') {
      if (r.value > max) max = r.value;
    } else if (firstErr === null) {
      firstErr = r.reason;
    }
  }
  if (max < 0n) {
    // Every sample errored — propagate the first reason.
    throw firstErr instanceof Error ? firstErr : new Error('quorum_read_all_failed');
  }
  return max;
}

/**
 * Read the treasury's USDL balance (raw token units, NOT decimal-adjusted).
 * Quorum-protected; see `readWithQuorum`.
 */
export async function readTreasuryUsdlBalance(): Promise<bigint> {
  const t = getTreasury();
  return readWithQuorum(() =>
    t.publicClient.readContract({
      address: env.FUNDED_USDL_ADDRESS as `0x${string}`,
      abi: ERC20_BALANCE_OF_ABI,
      functionName: 'balanceOf',
      args: [t.address],
    }),
  );
}

/** Read the treasury's native PAX balance in wei. Quorum-protected. */
export async function readTreasuryPaxBalance(): Promise<bigint> {
  const t = getTreasury();
  return readWithQuorum(() => t.publicClient.getBalance({ address: t.address }));
}

/**
 * Read an arbitrary wallet's USDL balance. Used by the evaluator to value
 * funded accounts. Quorum-protected — this is the read that, when stale,
 * caused the "instantly breached on provision" bug.
 */
export async function readUsdlBalance(walletAddress: `0x${string}`): Promise<bigint> {
  const t = getTreasury();
  return readWithQuorum(() =>
    t.publicClient.readContract({
      address: env.FUNDED_USDL_ADDRESS as `0x${string}`,
      abi: ERC20_BALANCE_OF_ABI,
      functionName: 'balanceOf',
      args: [walletAddress],
    }),
  );
}

/** Read an arbitrary wallet's native PAX balance in wei. Quorum-protected. */
export async function readPaxBalance(walletAddress: `0x${string}`): Promise<bigint> {
  const t = getTreasury();
  return readWithQuorum(() => t.publicClient.getBalance({ address: walletAddress }));
}

/**
 * Wait for a tx to be included. Used by the funding flow so the API only
 * returns success after both transfers actually land.
 *
 * Timeout: 30s (~15 blocks at 2s block time) — generous for a transient
 * RPC stutter but short enough that a stuck mempool surfaces as an error.
 */
export async function waitForTxReceipt(hash: Hex, timeoutMs = 30_000): Promise<void> {
  const t = getTreasury();
  await t.publicClient.waitForTransactionReceipt({
    hash,
    timeout: timeoutMs,
    // Confirmations = 1 is fine on chain 125's fast-finality consensus.
    confirmations: 1,
  });
}
