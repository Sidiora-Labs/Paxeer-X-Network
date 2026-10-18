/**
 * Module augmentation: maps "Transfer.DepositAndWithdraw" to its props type.
 * Import this file to enable typed props in
 * createInterceptor('Transfer.DepositAndWithdraw', (Original, props, api) => ...).
 */
import type { DepositAndWithdrawProps } from "./index";

declare module "@paxeer-network/plugin-core" {
  interface InterceptorTargetPropsMap {
    "Transfer.DepositAndWithdraw": DepositAndWithdrawProps;
  }
}
