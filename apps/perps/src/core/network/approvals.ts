import type { Address } from "viem";

import { PRECOMPILES } from "./precompiles";

export const PERPS_SPENDER: Address = PRECOMPILES.perps;
export const LAUNCHPAD_SPENDER: Address = PRECOMPILES.launchpad;
export const SPOT_SPENDERS = {
  market: PRECOMPILES.spot.PECOR,
  limit: PRECOMPILES.spot.PECOROrders,
  stop: PRECOMPILES.spot.PECORStopOrders,
} as const satisfies Record<string, Address>;

const MAX_UINT256 = (1n << 256n) - 1n;

/** A token of a call: an ERC20 address, or null for native value. */
export type ApprovalToken = Address | null;

/** Addresses of the PAX and SID pointers, which the precompiles move by bank transfer. */
export type NativeTokens = ReadonlySet<Address>;

export interface ApproveCall {
  address: Address;
  functionName: "approve";
  args: readonly [spender: Address, amount: bigint];
}

export type ApprovalPlan = readonly ApproveCall[];

function isNative(token: ApprovalToken, native: NativeTokens): boolean {
  if (token === null) return true;
  const lower = token.toLowerCase();
  for (const pointer of native) {
    if (pointer.toLowerCase() === lower) return true;
  }
  return false;
}

function checkAmount(amount: bigint): void {
  if (amount < 0n || amount >= MAX_UINT256) {
    throw new RangeError(`approval amount must be in [0, 2^256 - 1), got ${amount}`);
  }
}

export function needsApproval(
  token: ApprovalToken,
  allowance: bigint,
  amount: bigint,
  native: NativeTokens,
): boolean {
  checkAmount(amount);
  if (isNative(token, native)) return false;
  return allowance < amount;
}

/**
 * Plan the approval that precedes a call pulling `amount` of `token` into `spender`.
 * The allowance is read only for an ERC20 outside the native set.
 */
export async function planApproval(
  token: ApprovalToken,
  spender: Address,
  allowance: () => Promise<bigint>,
  amount: bigint,
  native: NativeTokens,
): Promise<ApprovalPlan> {
  checkAmount(amount);
  if (token === null || isNative(token, native)) return [];
  if (!needsApproval(token, await allowance(), amount, native)) return [];
  return [{ address: token, functionName: "approve", args: [spender, amount] }];
}
