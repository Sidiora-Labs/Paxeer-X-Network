import { Chain, ConnectedChain } from "@paxeer-network/hooks";

export enum DepositAction {
  Deposit,
  Approve,
  ApproveAndDeposit,
  // Increase,
}

export enum WithdrawTo {
  /** withdraw to web3 wallet */
  Wallet = "wallet",
  /** withdraw to other account id */
  Account = "accountId",
}

export type InputStatus = "error" | "warning" | "success" | "default";

export type CurrentChain = Pick<ConnectedChain, "namespace"> & {
  id: number;
  info?: Chain;
};
