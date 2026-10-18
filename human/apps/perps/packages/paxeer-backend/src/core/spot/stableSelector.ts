/* ══════════════════════════════════════════════════════════════════════
   Stablecoin selector for PECOR market orders.

   PECOR's `marketBuy` / `marketSell` accept ONE stablecoin per call.
   The UI abstracts USDL / USDC / USDT into a unified "USD" balance, so
   somebody has to pick which stable each trade actually settles in. We
   make that choice deterministically at submit time:

     · BUY  → pick the stable the user already holds the most of. This
              minimizes the number of distinct stables left over in the
              user's wallet after trading.
     · SELL → pick the stable with the deepest vault liquidity. This
              maximizes the chance the engine can fill the user's sell
              at the quoted price without hitting a utilization cap.

   Both fns are pure — the hook layer fetches balances / liquidity and
   passes them in. Tiebreakers fall back to `SPOT_STABLECOIN_LIST` order
   (USDL > USDC > USDT) for full determinism.
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";
import {
  SPOT_STABLECOIN_LIST,
  type SpotTokenMeta,
} from "@paxeer-network/paxeer-backend/core/spot/addresses";

/* ── public types ──────────────────────────────────────────────── */

/** Map of stablecoin address (lowercased) → balance / liquidity bigint. */
export type StableAmountMap = Readonly<Record<string, bigint>>;

export interface SelectStableForBuyInput {
  /** Amount of stable the user intends to spend (6-decimal scale). */
  amountUsd: bigint;
  /**
   * User's stablecoin balances keyed by lowercased on-chain address.
   * Missing keys are treated as zero balance.
   */
  balances: StableAmountMap;
}

export interface SelectStableForSellInput {
  /** Expected stable output the trade will produce (6-decimal scale). */
  expectedUsd: bigint;
  /**
   * Available vault liquidity per stable, keyed by lowercased address.
   * Source: `PECORQuoter.getLiquidityInfo(stable).available`.
   * Missing keys are treated as zero liquidity.
   */
  vaultLiquidity: StableAmountMap;
}

export interface StableSelection {
  /** Chosen stablecoin metadata. */
  stable: SpotTokenMeta;
  /** True when the chosen stable can fully cover the trade. */
  sufficient: boolean;
  /** The relevant amount for this side — balance for buys, liquidity for sells. */
  available: bigint;
}

/* ── selectors ─────────────────────────────────────────────────── */

/**
 * Pick the stablecoin to spend on a market BUY. Strategy:
 *
 *   1. Filter to stables whose balance ≥ `amountUsd`.
 *   2. If any qualify → take the one with the highest balance.
 *   3. If none qualify → take the one with the highest balance
 *      regardless and mark `sufficient: false`. The UI must surface
 *      an "insufficient USD" error before submitting.
 *
 * Ties broken by `SPOT_STABLECOIN_LIST` order for determinism.
 *
 * When `amountUsd === 0n` (preview mode), returns the user's
 * highest-balance stable so the form can render a stable hint.
 */
export function selectStableForBuy(
  input: SelectStableForBuyInput,
): StableSelection {
  const { amountUsd, balances } = input;

  const ranked = SPOT_STABLECOIN_LIST.map((stable) => ({
    stable,
    balance: balances[stable.address.toLowerCase()] ?? 0n,
  }));

  // Phase 1: stables that fully cover the order, max balance wins.
  if (amountUsd > 0n) {
    const sufficient = ranked
      .filter((r) => r.balance >= amountUsd)
      .sort((a, b) => compareDescending(a.balance, b.balance));
    if (sufficient.length > 0) {
      return {
        stable: sufficient[0].stable,
        sufficient: true,
        available: sufficient[0].balance,
      };
    }
  }

  // Phase 2: fall through to "best effort" — highest balance overall.
  const best = ranked.sort((a, b) => compareDescending(a.balance, b.balance))[0];
  return {
    stable: best.stable,
    sufficient: amountUsd === 0n ? true : best.balance >= amountUsd,
    available: best.balance,
  };
}

/**
 * Pick the stablecoin to settle a market SELL into. Strategy mirrors
 * `selectStableForBuy` but ranks by **vault liquidity** instead of
 * user balance — the engine needs enough free stable in its reserves
 * to pay out the trader.
 *
 * Tiebreaker is again `SPOT_STABLECOIN_LIST` order.
 */
export function selectStableForSell(
  input: SelectStableForSellInput,
): StableSelection {
  const { expectedUsd, vaultLiquidity } = input;

  const ranked = SPOT_STABLECOIN_LIST.map((stable) => ({
    stable,
    liquidity: vaultLiquidity[stable.address.toLowerCase()] ?? 0n,
  }));

  if (expectedUsd > 0n) {
    const sufficient = ranked
      .filter((r) => r.liquidity >= expectedUsd)
      .sort((a, b) => compareDescending(a.liquidity, b.liquidity));
    if (sufficient.length > 0) {
      return {
        stable: sufficient[0].stable,
        sufficient: true,
        available: sufficient[0].liquidity,
      };
    }
  }

  const best = ranked.sort((a, b) => compareDescending(a.liquidity, b.liquidity))[0];
  return {
    stable: best.stable,
    sufficient: expectedUsd === 0n ? true : best.liquidity >= expectedUsd,
    available: best.liquidity,
  };
}

/* ── utilities ─────────────────────────────────────────────────── */

function compareDescending(a: bigint, b: bigint): number {
  if (a > b) return -1;
  if (a < b) return 1;
  return 0;
}

/**
 * Sum a stable-amount map. Useful for the unified "USD" balance the
 * portfolio panel shows above the order form.
 */
export function sumStableAmounts(map: StableAmountMap): bigint {
  let total = 0n;
  for (const v of Object.values(map)) total += v;
  return total;
}

/**
 * Build a `StableAmountMap` from a list of `{ address, amount }`
 * tuples — keys are lowercased automatically. Helper used by hooks
 * that source balances from the indexer (REST/WS) before passing them
 * into the selector.
 */
export function buildStableAmountMap(
  entries: ReadonlyArray<{ address: Address | string; amount: bigint }>,
): StableAmountMap {
  const out: Record<string, bigint> = {};
  for (const { address, amount } of entries) {
    out[address.toLowerCase()] = amount;
  }
  return out;
}
