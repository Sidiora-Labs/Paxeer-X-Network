/* ══════════════════════════════════════════════════════════════════════
   Stablecoin selector for PECOR market orders.

   PECOR's `marketBuy` / `marketSell` accept ONE stablecoin per call, and
   USDL is the only stable token of the venue, so both selectors settle
   in USDL and report whether its balance (buys) or vault liquidity
   (sells) covers the trade.

   Both fns are pure — the hook layer fetches balances / liquidity and
   passes them in.
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";
import { SPOT_TOKENS, type SpotTokenMeta } from "@/core/spot/addresses";

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

const USDL: SpotTokenMeta = SPOT_TOKENS.USDL;

function selectUsdl(required: bigint, amounts: StableAmountMap): StableSelection {
  const available = amounts[USDL.address.toLowerCase()] ?? 0n;
  return { stable: USDL, sufficient: available >= required, available };
}

/**
 * Pick the stablecoin to spend on a market BUY: USDL, sufficient when the
 * user's USDL balance covers `amountUsd` (always when `amountUsd === 0n`,
 * preview mode).
 */
export function selectStableForBuy(input: SelectStableForBuyInput): StableSelection {
  return selectUsdl(input.amountUsd, input.balances);
}

/**
 * Pick the stablecoin to settle a market SELL into: USDL, sufficient when
 * the USDL vault liquidity covers `expectedUsd`.
 */
export function selectStableForSell(input: SelectStableForSellInput): StableSelection {
  return selectUsdl(input.expectedUsd, input.vaultLiquidity);
}

/* ── utilities ─────────────────────────────────────────────────── */

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
