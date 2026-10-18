/* ══════════════════════════════════════════════════════════════════════
   ERC-20 allowance helpers for PECOR spot trading.

   PECOR has no native EIP-2612 permit and no Permit2 integration on the
   spot contracts, so every spend is gated by a standard `approve` →
   `transferFrom` flow. For UX we issue a one-time `MaxUint256` approval
   per (token, spender) pair, which means subsequent trades are a single
   tx with no allowance check overhead.

   On embedded wallets the approval is auto-confirmed silently — the
   user never sees a popup. On injected wallets (MetaMask / Rabby /
   Paxport-injected) it surfaces a single approval prompt that's good
   for that token forever.

   The hook layer is intentionally stateless — every call hits chain.
   Higher-level hooks (`useSpotOrders`) cache the result of a prior
   `ensureAllowance` for the duration of a single submit pipeline.
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";
import { erc20Abi } from "@paxeer-network/paxeer-backend/core/perps/config";
import {
  readContract,
  writeContract,
  waitForTx,
  type WaitForTxResult,
} from "@paxeer-network/paxeer-backend/hooks/perps/useContract";

/* ── constants ─────────────────────────────────────────────────── */

/** uint256 max — used as the standard "infinite" allowance. */
export const MAX_UINT256: bigint = (1n << 256n) - 1n;

/**
 * Threshold below which we trigger a re-approval even though the
 * allowance is technically non-zero. Most users start with `MaxUint256`
 * and never burn through it, but if a custom approval drops below this
 * we issue a fresh max approval rather than tracking partial allowance.
 */
const ALLOWANCE_TOPUP_THRESHOLD: bigint = (MAX_UINT256 / 2n);

/* ── read ──────────────────────────────────────────────────────── */

/** Read the user's current allowance for `(token, spender)` pair. */
export async function getAllowance(params: {
  token: Address;
  owner: Address;
  spender: Address;
}): Promise<bigint> {
  const { token, owner, spender } = params;
  const result = await readContract({
    address: token,
    abi: erc20Abi,
    functionName: "allowance",
    args: [owner, spender],
  });
  return typeof result === "bigint" ? result : 0n;
}

/* ── write ─────────────────────────────────────────────────────── */

/**
 * Issue a `MaxUint256` approval for the given `(token, spender)` pair.
 * Returns when the receipt has been observed or the finality window
 * expires. Throws on revert or timeout.
 */
export async function approveMax(params: {
  token: Address;
  spender: Address;
}): Promise<WaitForTxResult> {
  const { token, spender } = params;
  const hash = await writeContract({
    address: token,
    abi: erc20Abi,
    functionName: "approve",
    args: [spender, MAX_UINT256],
  });
  return waitForTx(hash);
}

/**
 * Issue a precise allowance for `(token, spender)`. Lower-level than
 * `approveMax` — exposed for advanced flows like Permit2 wrappers that
 * want exact-amount approvals. The order pipeline always uses
 * `approveMax` for v1.
 */
export async function approveExact(params: {
  token: Address;
  spender: Address;
  amount: bigint;
}): Promise<WaitForTxResult> {
  const { token, spender, amount } = params;
  const hash = await writeContract({
    address: token,
    abi: erc20Abi,
    functionName: "approve",
    args: [spender, amount],
  });
  return waitForTx(hash);
}

/* ── high-level convenience ────────────────────────────────────── */

export interface EnsureAllowanceResult {
  /** True when a new approval was issued in this call. */
  approved: boolean;
  /** Current allowance after the call (always ≥ `amount` on success). */
  allowance: bigint;
  /** Receipt of the approval tx, if one was issued. */
  receipt?: WaitForTxResult;
}

/**
 * Ensure the user has at least `amount` allowance for the given
 * `(token, spender)` pair, issuing a `MaxUint256` approval if needed.
 *
 * Skips the approval write when:
 *   · `amount === 0n` (no spend planned)
 *   · existing allowance ≥ `amount` AND ≥ `ALLOWANCE_TOPUP_THRESHOLD`
 *
 * Returns the updated allowance plus the approval receipt (when one
 * was issued). Throws if the approval tx reverts or times out.
 */
export async function ensureAllowance(params: {
  token: Address;
  owner: Address;
  spender: Address;
  amount: bigint;
}): Promise<EnsureAllowanceResult> {
  const { token, owner, spender, amount } = params;

  if (amount === 0n) {
    return { approved: false, allowance: 0n };
  }

  const current = await getAllowance({ token, owner, spender });

  // Existing allowance is healthy — short-circuit.
  if (current >= amount && current >= ALLOWANCE_TOPUP_THRESHOLD) {
    return { approved: false, allowance: current };
  }

  const receipt = await approveMax({ token, spender });
  return {
    approved: true,
    allowance: MAX_UINT256,
    receipt,
  };
}

/* ── hook surface ──────────────────────────────────────────────── */

/**
 * Pure-function-style hook. Returns the public approval helpers bound
 * for ergonomic destructuring in submit pipelines:
 *
 *   const { ensureAllowance } = useSpotApprovals();
 *   await ensureAllowance({ token: USDL, owner, spender: PECOR, amount });
 */
export function useSpotApprovals() {
  return {
    getAllowance,
    approveMax,
    approveExact,
    ensureAllowance,
    MAX_UINT256,
  };
}
