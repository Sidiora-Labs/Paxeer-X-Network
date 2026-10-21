/* ══════════════════════════════════════════════════════════════════════
   ERC-20 allowance helpers for the spot and perpetuals precompiles.

   The precompiles pull ERC-20 inputs with `transferFrom` against the
   allowance the user gives to the precompile address. Every approval
   is planned by `planApproval` and requests exactly the amount of the
   call it precedes. The PAX and SID pointers and native value move by
   bank transfer, so no allowance is read or requested for them.

   The hook layer is intentionally stateless — every call hits chain.
   ══════════════════════════════════════════════════════════════════════ */

import type { Address } from "viem";
import { erc20Abi } from "@/core/perps/config";
import {
  planApproval,
  type ApprovalToken,
  type NativeTokens,
} from "@/core/network/approvals";
import { NATIVE_PAX_ADDRESS, SPOT_TOKENS } from "@/core/spot/addresses";
import {
  readContract,
  writeContract,
  waitForTx,
  type WaitForTxResult,
} from "@/hooks/perps/useContract";

/* ── native set ────────────────────────────────────────────────── */

/** Addresses of the token table that the precompiles move by bank transfer. */
export const SPOT_NATIVE_TOKENS: NativeTokens = new Set<Address>(
  Object.values(SPOT_TOKENS)
    .filter((token) => token.isNative)
    .map((token) => token.address),
);

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
 * Issue an allowance of exactly `amount` for `(token, spender)`.
 * Returns when the receipt has been observed or the finality window
 * expires. Throws on revert or timeout.
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
  /** Allowance after the call, or null when the token needs none (native coin or value). */
  allowance: bigint | null;
  /** Receipt of the approval tx, if one was issued. */
  receipt?: WaitForTxResult;
}

/**
 * Ensure the user has at least `amount` allowance for `(token, spender)`,
 * approving exactly `amount` through `approveExact` when it is short.
 *
 * No allowance is read for `null` (native value) or a member of
 * `native`. Throws RangeError for an amount outside [0, 2^256 - 1) and
 * rethrows if the approval tx reverts or times out.
 */
export async function ensureAllowance(params: {
  token: ApprovalToken;
  owner: Address;
  spender: Address;
  amount: bigint;
  native?: NativeTokens;
}): Promise<EnsureAllowanceResult> {
  const { owner, spender, amount, native = SPOT_NATIVE_TOKENS } = params;
  // NATIVE_PAX_ADDRESS is the native-value sentinel: PAX sent as value has no allowance.
  const token = params.token?.toLowerCase() === NATIVE_PAX_ADDRESS ? null : params.token;

  let current: bigint | null = null;
  const plan = await planApproval(
    token,
    spender,
    async () => {
      if (token === null) {
        throw new Error("ensureAllowance: native value has no allowance");
      }
      current = await getAllowance({ token, owner, spender });
      return current;
    },
    amount,
    native,
  );

  if (plan.length === 0) {
    return { approved: false, allowance: current };
  }

  let receipt: WaitForTxResult | undefined;
  for (const call of plan) {
    const [approvedSpender, approvedAmount] = call.args;
    receipt = await approveExact({
      token: call.address,
      spender: approvedSpender,
      amount: approvedAmount,
    });
  }
  return { approved: true, allowance: amount, receipt };
}

/* ── hook surface ──────────────────────────────────────────────── */

/**
 * Pure-function-style hook. Returns the public approval helpers bound
 * for ergonomic destructuring in submit pipelines:
 *
 *   const { ensureAllowance } = useSpotApprovals();
 *   await ensureAllowance({ token: USDL, owner, spender: SPOT_SPENDERS.market, amount });
 */
export function useSpotApprovals() {
  return {
    getAllowance,
    approveExact,
    ensureAllowance,
  };
}
