import type { Address } from "viem";

import { accountViewFromState, type AccountView } from "@/core/perps/accountView";
import {
  buildDepositPlan,
  buildGetAccount,
  buildWithdraw,
  buildWithdrawAll,
  type AccountState,
} from "@/core/perps/calls";
import { PERPS_SPENDER } from "@/core/network/approvals";
import { COLLATERAL_TOKENS, erc20Abi } from "@/core/perps/config";
import { SPOT_NATIVE_TOKENS } from "@/hooks/spot/useSpotApprovals";
import { readContract, writeContract, waitForTx, type WaitForTxResult } from "./useContract";

const USDL: Address = COLLATERAL_TOKENS.usdl.address;

export type DepositStep = "approving" | "executing";

/** getAccount(account) read directly at 0x1015. */
export async function getAccount(account: Address): Promise<AccountView> {
  const state = (await readContract(buildGetAccount(account))) as AccountState;
  return accountViewFromState(state);
}

/** USDL in the wallet, base units. */
export async function getWalletBalance(account: Address): Promise<bigint> {
  return (await readContract({
    address: USDL,
    abi: erc20Abi,
    functionName: "balanceOf",
    args: [account],
  })) as bigint;
}

/** The USDL allowance of `account` to 0x1015, base units. */
export async function getAllowance(account: Address): Promise<bigint> {
  return (await readContract({
    address: USDL,
    abi: erc20Abi,
    functionName: "allowance",
    args: [account, PERPS_SPENDER],
  })) as bigint;
}

/**
 * Deposit `amount` USDL base units into the account at 0x1015, approving exactly
 * `amount` first when the allowance is short. The first deposit opens the account.
 */
export async function deposit(
  account: Address,
  amount: bigint,
  onStep?: (step: DepositStep) => void,
): Promise<WaitForTxResult> {
  const plan = await buildDepositPlan(amount, () => getAllowance(account), SPOT_NATIVE_TOKENS);
  let receipt: WaitForTxResult | undefined;
  for (const call of plan) {
    if (call.functionName === "approve") {
      onStep?.("approving");
      receipt = await waitForTx(await writeContract({ ...call, abi: erc20Abi }));
    } else {
      onStep?.("executing");
      receipt = await waitForTx(await writeContract(call));
    }
  }
  if (!receipt) throw new Error("deposit plan sent no transaction");
  return receipt;
}

/** Withdraw `amount` USDL base units from the account at 0x1015. */
export async function withdraw(amount: bigint): Promise<WaitForTxResult> {
  return waitForTx(await writeContract(buildWithdraw(amount)));
}

/** Withdraw getAccount(account).withdrawable, never the idle balance. */
export async function withdrawAll(account: Address): Promise<WaitForTxResult> {
  return waitForTx(await writeContract(buildWithdrawAll(await getAccount(account))));
}

export function useAccount() {
  return { deposit, withdraw, withdrawAll, getAccount, getWalletBalance, getAllowance };
}
