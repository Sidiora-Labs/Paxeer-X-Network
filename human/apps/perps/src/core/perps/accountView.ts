import type { Address } from "viem";

import type { AccountState } from "./calls";
import { COLLATERAL_TOKENS } from "./config";

/** The cross-margin account of getAccount(account) at 0x1015. */
export interface AccountView {
  /** USDL base units (6 decimals). */
  collateral: bigint;
  /** 18 decimals; negative when losses exceed collateral. */
  equityUsd: bigint;
  /** 18 decimals. */
  initialMarginUsd: bigint;
  /** 18 decimals. */
  maintenanceMarginUsd: bigint;
  /** USDL base units (6 decimals); the amount a withdraw-all requests. */
  withdrawable: bigint;
}

export function accountViewFromState(accountState: AccountState): AccountView {
  return {
    collateral: accountState.collateral,
    equityUsd: accountState.equityUsd,
    initialMarginUsd: accountState.initialMarginUsd,
    maintenanceMarginUsd: accountState.maintenanceMarginUsd,
    withdrawable: accountState.withdrawable,
  };
}

/** The collateral the account trades on: USDL, the single collateral token. */
export interface PickedCollateral {
  token: Address;
  symbol: string;
  decimals: number;
  /** USDL base units (6 decimals) of getAccount(account).collateral. */
  collateral: bigint;
}

export function pickCollateral(accountState: Pick<AccountState, "collateral">): PickedCollateral {
  const { address, symbol, decimals } = COLLATERAL_TOKENS.usdl;
  return { token: address, symbol, decimals, collateral: accountState.collateral };
}
