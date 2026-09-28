import type { FastifyBaseLogger } from 'fastify';
import { env } from '../env.js';
import { getPool, query } from '../db/pool.js';
import {
  applyEvaluatorUpdate,
  findTier,
  listAccountsForEvaluation,
  type EvaluatorUpdate,
  type FundedAccountRow,
  type FundedAccountStatus,
} from '../db/fundedAccounts.js';
import { getWalletEquityUsd } from '../treasury/paxscan.js';

/**
 * Drawdown + scale/payout evaluator.
 *
 * Runs every FUNDED_EVALUATOR_INTERVAL_MS in the API process. Multi-worker
 * safety is handled by a Postgres advisory lock — only one worker actually
 * evaluates per tick; the others fast-return.
 *
 * Each tick:
 *   1. Take the advisory lock. Skip if busy.
 *   2. List all live funded_accounts ordered by last_eval_at (oldest first).
 *   3. For each: ask Paxscan for the wallet's USD equity across native PAX
 *      and every indexed ERC-20 (USDL, SID, USDC, vault shares, anything).
 *      (Stage 1: spot only. Stage 2 will add open perp position value via
 *      the Diamond's QuoterFacet.quoteClosePosition, since perp collateral
 *      is internal Diamond accounting and not visible to Blockscout.)
 *   4. Update peak_value_usd if increased.
 *   5. If we crossed a UTC day boundary, snapshot daily_start_value_usd.
 *   6. Apply drawdown rules:
 *      - equity < daily_start - max_daily_dd_bps  → status='breached_daily'
 *      - equity < starting   - max_total_dd_bps   → status='breached_max'
 *   7. Apply milestone rules:
 *      - peak >= payout_threshold → 'payout_eligible'
 *      - peak >= scale_threshold  → 'scale_eligible'
 *      (highest milestone wins; downgrades only happen via breach)
 *   8. Persist the update.
 *   9. Release the advisory lock.
 *
 * The advisory lock is per-database, hashtext'd from a constant string. If
 * the worker crashes mid-tick, Postgres releases the session-scoped lock
 * automatically on connection close — no manual cleanup required.
 */

// Stable lock key. Postgres's pg_try_advisory_lock takes a bigint, so we hash
// the constant once at module load and reuse the same int across ticks.
const LOCK_NAME = 'paxeer:funded_evaluator';

// Bound how many accounts we value per tick. With a 10s tick and 100 accounts
// per tick, the system valuates 36k accounts/hour even on the cheapest RPC.
// Bump if growth requires it; on chain 125 the RPC handles parallel reads
// well so this number can grow significantly before becoming a bottleneck.
const ACCOUNTS_PER_TICK = 200;

// -----------------------------------------------------------------------------
// Bootstrap
// -----------------------------------------------------------------------------

let timer: NodeJS.Timeout | null = null;
let running = false;

export interface EvaluatorHandle {
  stop: () => void;
}

/**
 * Start the evaluator. Called once per worker from `index.ts` after the DB
 * is ready. Safe to call from every worker — the advisory lock prevents
 * duplicate work.
 *
 * Returns a handle whose stop() halts the loop on graceful shutdown.
 */
export function startFundedEvaluator(logger: FastifyBaseLogger): EvaluatorHandle {
  if (timer) {
    logger.warn('[funded-eval] already started — ignoring');
    return { stop: stopEvaluator };
  }
  logger.info(
    `[funded-eval] started, interval=${env.FUNDED_EVALUATOR_INTERVAL_MS}ms`,
  );
  timer = setInterval(() => {
    void runTick(logger);
  }, env.FUNDED_EVALUATOR_INTERVAL_MS);
  // Fire one immediately so a freshly started worker doesn't wait a full tick
  // before catching up.
  void runTick(logger);
  return { stop: stopEvaluator };
}

function stopEvaluator(): void {
  if (timer) {
    clearInterval(timer);
    timer = null;
  }
}

// -----------------------------------------------------------------------------
// Tick body
// -----------------------------------------------------------------------------

async function runTick(logger: FastifyBaseLogger): Promise<void> {
  if (running) return; // re-entrancy guard within this worker
  running = true;
  const client = await getPool().connect();
  try {
    const { rows } = await client.query<{ ok: boolean }>(
      `select pg_try_advisory_lock(hashtextextended($1, 0)) as ok`,
      [LOCK_NAME],
    );
    if (!rows[0]?.ok) {
      // Another worker holds the lock this tick. Quiet — happens every tick.
      return;
    }
    try {
      const accounts = await listAccountsForEvaluation(ACCOUNTS_PER_TICK);
      if (accounts.length === 0) return;
      logger.debug({ count: accounts.length }, '[funded-eval] tick');
      for (const account of accounts) {
        try {
          await evaluateOne(account);
        } catch (err) {
          // Per-account failure must NOT abort the rest of the tick.
          logger.warn(
            { err, accountId: account.id },
            '[funded-eval] account evaluation failed',
          );
        }
      }
    } finally {
      // Release explicitly; not strictly required because session-end releases,
      // but explicit makes the lifetime obvious.
      await client.query(`select pg_advisory_unlock(hashtextextended($1, 0))`, [LOCK_NAME]);
    }
  } catch (err) {
    logger.warn({ err }, '[funded-eval] tick errored at lock acquisition');
  } finally {
    client.release();
    running = false;
  }
}

// -----------------------------------------------------------------------------
// Per-account valuation + state transition
// -----------------------------------------------------------------------------

async function evaluateOne(account: FundedAccountRow): Promise<void> {
  // Defensive guard: never evaluate an un-funded row, even if it leaked past
  // the listAccountsForEvaluation filter. Reading balances on a wallet that
  // hasn't been funded yet returns 0/0 and would immediately breach the
  // account on `equity <= starting - maxDD`. This is the root cause of the
  // "instantly breached on provision" bug.
  if (!account.funding_tx_hashes?.usdl || !account.funding_tx_hashes?.pax) {
    return;
  }

  // Look up the wallet address and tier params we need.
  const [wallet, tier] = await Promise.all([
    getWalletAddressById(account.wallet_id),
    findTier(account.tier_id),
  ]);
  if (!wallet || !tier) return;

  // Stage 1 valuation: full-spot equity via Paxscan (Blockscout-based), which
  // returns native PAX + every indexed ERC-20 already priced in USD via the
  // explorer's per-token exchange_rate. One HTTP call replaces N RPC reads,
  // and crucially it sees ALL tokens (USDL, SID, USDC, vault shares, ...)
  // not just USDL — so a user who swapped their starting USDL for SID is
  // valued correctly instead of being read as $0.
  //
  // On Paxscan failure we propagate the error to runTick, which logs a
  // warning and SKIPS this account for this tick. We must NOT fall back to
  // a partial valuation (e.g. USDL-only) — that would re-introduce the
  // wrong-breach bug the moment a user holds anything other than USDL.
  // Next tick (~10s later) will retry.
  const equityUsd = await getWalletEquityUsd(wallet.address);

  const decision = decideTransition({
    account,
    tier: {
      max_daily_dd_bps: tier.max_daily_dd_bps,
      max_total_dd_bps: tier.max_total_dd_bps,
      scale_threshold_usd: Number(tier.scale_threshold_usd),
      payout_threshold_usd: Number(tier.payout_threshold_usd),
    },
    equity_usd: equityUsd,
    now: new Date(),
  });

  const update: EvaluatorUpdate = {
    id: account.id,
    current_value_usd: decision.current_value_usd,
  };
  if (decision.peak_value_usd !== undefined) update.peak_value_usd = decision.peak_value_usd;
  if (decision.status !== undefined) update.status = decision.status;
  if (decision.breached_reason !== undefined) update.breached_reason = decision.breached_reason;
  if (decision.daily_start_value_usd !== undefined) {
    update.daily_start_value_usd = decision.daily_start_value_usd;
    update.daily_start_at = decision.daily_start_at;
  }
  await applyEvaluatorUpdate(update);
}

// -----------------------------------------------------------------------------
// Pure state-transition logic — no chain, no DB, no time other than `now`.
// Easy to unit test. evaluateOne() owns the IO around it.
// -----------------------------------------------------------------------------

export interface TransitionInput {
  account: Pick<
    FundedAccountRow,
    'status' | 'starting_value_usd' | 'peak_value_usd' | 'daily_start_value_usd' | 'daily_start_at'
  >;
  tier: {
    max_daily_dd_bps: number;
    max_total_dd_bps: number;
    scale_threshold_usd: number;
    payout_threshold_usd: number;
  };
  equity_usd: number;
  now: Date;
}

export interface TransitionDecision {
  current_value_usd: string;
  peak_value_usd?: string;
  status?: FundedAccountStatus;
  breached_reason?: string;
  daily_start_value_usd?: string;
  daily_start_at?: string;
}

export function decideTransition(input: TransitionInput): TransitionDecision {
  const { account, tier, equity_usd, now } = input;

  const startingUsd = Number(account.starting_value_usd);
  const peakBeforeUsd = Number(account.peak_value_usd);
  const peakUsd = Math.max(peakBeforeUsd, equity_usd);

  // Daily window roll. The first eval ever lacks a daily_start_at; treat that
  // as a fresh window starting at the current equity.
  const dailyStartAt = account.daily_start_at ? new Date(account.daily_start_at) : null;
  const dailyRolloverNeeded = dailyStartAt === null || crossedUtcDayBoundary(dailyStartAt, now);

  // Daily DD compares against the daily start, which is reset on rollover.
  // If a rollover is happening THIS tick, use current equity as the new
  // baseline (no breach can fire on the same tick as a rollover).
  const dailyStartUsd = dailyRolloverNeeded
    ? equity_usd
    : Number(account.daily_start_value_usd ?? startingUsd);

  // Status decision — breach checks first (irreversible), then milestones.
  let nextStatus: FundedAccountStatus | undefined;
  let breachReason: string | undefined;

  const maxDdUsd = startingUsd * (tier.max_total_dd_bps / 10_000);
  const dailyDdUsd = dailyStartUsd * (tier.max_daily_dd_bps / 10_000);

  if (equity_usd <= startingUsd - maxDdUsd) {
    if (account.status !== 'breached_max') {
      nextStatus = 'breached_max';
      breachReason = `equity ${fmt(equity_usd)} <= starting ${fmt(startingUsd)} - maxDD ${fmt(maxDdUsd)} (${tier.max_total_dd_bps}bps)`;
    }
  } else if (!dailyRolloverNeeded && equity_usd <= dailyStartUsd - dailyDdUsd) {
    if (account.status !== 'breached_daily') {
      nextStatus = 'breached_daily';
      breachReason = `equity ${fmt(equity_usd)} <= daily_start ${fmt(dailyStartUsd)} - dailyDD ${fmt(dailyDdUsd)} (${tier.max_daily_dd_bps}bps)`;
    }
  } else {
    // Milestones based on peak (high-watermark) so brief dips don't downgrade.
    let target: FundedAccountStatus = 'active';
    if (peakUsd >= tier.payout_threshold_usd) target = 'payout_eligible';
    else if (peakUsd >= tier.scale_threshold_usd) target = 'scale_eligible';
    if (target !== account.status) nextStatus = target;
  }

  const decision: TransitionDecision = {
    current_value_usd: equity_usd.toFixed(6),
  };
  if (peakUsd > peakBeforeUsd) decision.peak_value_usd = peakUsd.toFixed(6);
  if (nextStatus !== undefined) {
    decision.status = nextStatus;
    if (breachReason !== undefined) decision.breached_reason = breachReason;
  }
  if (dailyRolloverNeeded) {
    decision.daily_start_value_usd = equity_usd.toFixed(6);
    decision.daily_start_at = now.toISOString();
  }
  return decision;
}

// -----------------------------------------------------------------------------
// Internal helpers
// -----------------------------------------------------------------------------

// unitsToUsdNumber + weiToUsdNumber are no longer called by evaluateOne
// (Paxscan returns USD-priced values directly). They are retained because
// `_internals` exports them for the existing unit-test suite, and they remain
// useful for the Stage-2 perp-position adapter where Diamond reads return
// raw token units that need to be re-priced via PriceOracle.getPrice().

function unitsToUsdNumber(units: bigint, decimals: number): number {
  // USDL is $1-pegged so units→USD is just unit-shift.
  // For 25_000_000_000 at 6dp this is 25_000.000000.
  const denom = 10n ** BigInt(decimals);
  const whole = units / denom;
  const frac = units % denom;
  return Number(whole) + Number(frac) / Number(denom);
}

function weiToUsdNumber(wei: bigint, decimals: number, usdPerToken: number): number {
  const denom = 10n ** BigInt(decimals);
  const whole = wei / denom;
  const frac = wei % denom;
  const tokenAmount = Number(whole) + Number(frac) / Number(denom);
  return tokenAmount * usdPerToken;
}

function fmt(n: number): string {
  return `$${n.toFixed(2)}`;
}

function crossedUtcDayBoundary(prev: Date, now: Date): boolean {
  const resetHour = env.FUNDED_DAILY_RESET_UTC_HOUR;
  const prevWindow = windowIndex(prev, resetHour);
  const nowWindow = windowIndex(now, resetHour);
  return nowWindow > prevWindow;
}

/** Integer index identifying which daily window a timestamp falls in. */
function windowIndex(d: Date, resetHour: number): number {
  // Offset so the day starts at resetHour UTC. Resulting integer increments
  // exactly once per crossing of that hour.
  const shifted = d.getTime() - resetHour * 3600_000;
  return Math.floor(shifted / 86_400_000);
}

/**
 * Lookup wallet address by funded_account.wallet_id. The accounts list
 * already gives us the id; chain reads only need the address.
 */
async function getWalletAddressById(
  walletId: string,
): Promise<{ address: `0x${string}` } | null> {
  const { rows } = await query<{ address: `0x${string}` }>(
    `select address from wallets where id = $1`,
    [walletId],
  );
  return rows[0] ?? null;
}

// -----------------------------------------------------------------------------
// Test hooks — exposed so a future test suite can drive the tick directly
// without timers. Not part of the public surface.
// -----------------------------------------------------------------------------

export const _internals = {
  evaluateOne,
  runTick,
  decideTransition,
  unitsToUsdNumber,
  weiToUsdNumber,
  crossedUtcDayBoundary,
  windowIndex,
};
