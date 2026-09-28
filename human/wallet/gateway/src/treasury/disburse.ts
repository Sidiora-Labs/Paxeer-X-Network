import type { Hex } from 'viem';
import { env } from '../env.js';
import { findTier } from '../db/fundedAccounts.js';
import { withWalletLock } from '../util/walletLock.js';
import {
  TREASURY_LOCK_KEY,
  TreasuryUnavailableError,
  readPaxBalance,
  readTreasuryPaxBalance,
  readTreasuryUsdlBalance,
  readUsdlBalance,
  treasuryErc20Transfer,
  treasuryNativeTransfer,
  waitForTxReceipt,
} from './index.js';

/**
 * Disburse the initial funding to a newly-provisioned funded wallet.
 *
 * Two sequential transfers (USDL → wallet, PAX → wallet) from the treasury
 * EOA, both serialised behind a single TREASURY_LOCK_KEY so concurrent
 * `/v1/funded/provision` requests can't collide on the nonce.
 *
 * If the second transfer fails after the first succeeded, the wallet ends up
 * partially funded. The caller (route layer) records the partial state in
 * `funded_accounts.funding_tx_hashes` so it can be reconciled by the
 * evaluator or by a manual operator action; we don't try to "rollback" a
 * successful on-chain transfer.
 *
 * Pre-flight: checks the treasury has sufficient inventory and bails out
 * BEFORE submitting any tx. This avoids burning gas on a guaranteed-failure
 * disbursement (e.g. when ops forgets to top up USDL).
 */

export interface DisburseTierResult {
  usdl_tx_hash: Hex;
  pax_tx_hash: Hex;
  tier_id: string;
  usdl_amount: string;       // string form of bigint (raw units, e.g. 6-dec USDL)
  pax_amount_wei: string;    // wei string
}

export class TreasuryInsufficientFundsError extends Error {
  constructor(
    public readonly asset: 'usdl' | 'pax',
    public readonly required: bigint,
    public readonly available: bigint,
  ) {
    super(
      `treasury insufficient ${asset.toUpperCase()}: need ${required.toString()}, have ${available.toString()}`,
    );
    this.name = 'TreasuryInsufficientFundsError';
  }
}

/**
 * Look up the tier and disburse initial funding to `recipient`.
 *
 * Throws:
 *   - TreasuryUnavailableError if FUNDED_TREASURY_PRIVATE_KEY is missing
 *   - TreasuryInsufficientFundsError if the treasury can't cover the disbursement
 *   - Error("unknown_tier") if tierId isn't registered
 *   - Error on RPC / chain failure (propagated as-is from viem)
 */
export async function disburseTier(args: {
  tierId: string;
  recipient: `0x${string}`;
}): Promise<DisburseTierResult> {
  const tier = await findTier(args.tierId);
  if (!tier) throw new Error(`unknown_tier: ${args.tierId}`);
  if (!tier.is_active) throw new Error(`tier_inactive: ${args.tierId}`);

  // Pre-flight: ensure treasury has the inventory for this disbursement.
  // We don't pre-flight gas for the transfers themselves — viem estimates
  // that — but it's a few thousand wei on chain 125 so any wallet with the
  // PAX_AMOUNT for the user grant has plenty of gas headroom too.
  const [usdlAvail, paxAvail] = await Promise.all([
    readTreasuryUsdlBalance(),
    readTreasuryPaxBalance(),
  ]);
  if (usdlAvail < tier.initial_usdl_units) {
    throw new TreasuryInsufficientFundsError('usdl', tier.initial_usdl_units, usdlAvail);
  }
  if (paxAvail < tier.initial_pax_wei) {
    throw new TreasuryInsufficientFundsError('pax', tier.initial_pax_wei, paxAvail);
  }

  // Serialise treasury sends so the nonce stays monotonic across parallel
  // provision requests.
  return withWalletLock(TREASURY_LOCK_KEY, async () => {
    const usdlHash = await treasuryErc20Transfer({
      token: env.FUNDED_USDL_ADDRESS as `0x${string}`,
      to: args.recipient,
      amount: tier.initial_usdl_units,
    });
    // Wait for USDL inclusion before submitting PAX so we don't accidentally
    // overlap nonces if `sendTransaction` returns before the chain's mempool
    // has registered the tx. Sequence: send → wait → send → wait. Slightly
    // slower than fire-and-forget but rock-solid.
    await waitForTxReceipt(usdlHash);

    const paxHash = await treasuryNativeTransfer({
      to: args.recipient,
      amount: tier.initial_pax_wei,
    });
    await waitForTxReceipt(paxHash);

    // Final gate: confirm the SAME publicClient the evaluator uses can
    // actually see the disbursed balances at head. On chain 125 we've
    // observed a brief window (≲1s, occasionally longer under load) where
    // waitForTxReceipt returns ok but a follow-up readContract still serves
    // the pre-transfer state. If we record funding_tx_hashes during that
    // window, the next evaluator tick reads `usdl=0` + `pax=15` for the
    // wallet, computes `equity=$197.55`, and trips the max-DD breach.
    //
    // Polling closes the race deterministically on the disbursement side
    // instead of papering over it with retry logic in the evaluator.
    await waitForBalancesVisible({
      recipient: args.recipient,
      expectedUsdl: tier.initial_usdl_units,
      expectedPax: tier.initial_pax_wei,
    });

    return {
      usdl_tx_hash: usdlHash,
      pax_tx_hash: paxHash,
      tier_id: tier.tier_id,
      usdl_amount: tier.initial_usdl_units.toString(),
      pax_amount_wei: tier.initial_pax_wei.toString(),
    };
  });
}

/**
 * Poll the public client (the one the evaluator also uses) until both balances
 * reflect the disbursement at head. Throws if the publicClient never catches
 * up within the timeout — far better to surface a clear error than to declare
 * success with state the evaluator can't see.
 *
 * 200ms polling cadence; 15s total budget. With chain 125's ~2s block time
 * and fast finality, the typical observed delay is one or two polls.
 */
async function waitForBalancesVisible(args: {
  recipient: `0x${string}`;
  expectedUsdl: bigint;
  expectedPax: bigint;
}): Promise<void> {
  const POLL_INTERVAL_MS = 200;
  const TIMEOUT_MS = 15_000;
  const start = Date.now();
  while (Date.now() - start < TIMEOUT_MS) {
    const [usdl, pax] = await Promise.all([
      readUsdlBalance(args.recipient),
      readPaxBalance(args.recipient),
    ]);
    if (usdl >= args.expectedUsdl && pax >= args.expectedPax) return;
    await new Promise((r) => setTimeout(r, POLL_INTERVAL_MS));
  }
  // Last-chance read to populate the error with the actual observed state.
  const [usdl, pax] = await Promise.all([
    readUsdlBalance(args.recipient),
    readPaxBalance(args.recipient),
  ]);
  throw new Error(
    `disbursement_visibility_timeout: ` +
      `expected usdl>=${args.expectedUsdl} pax>=${args.expectedPax}, ` +
      `observed usdl=${usdl} pax=${pax} after ${TIMEOUT_MS}ms`,
  );
}

/** Re-export for route layer to translate into clean 503s. */
export { TreasuryUnavailableError };
