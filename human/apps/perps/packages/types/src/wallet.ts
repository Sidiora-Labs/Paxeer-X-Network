import { API } from "./types/api";

// export type Chain = {
//   id: string;
//   name?: string;
// };

export type CurrentChain = {
  id: number;
  info: API.Chain;
};

export type WalletChainChangeState = {
  isTestnet: boolean;
  isWalletConnected: boolean;
  isWalletConnectionPending: boolean;
};

export const WALLET_CHAIN_CHANGE_PENDING =
  "wallet:chain-change-pending" as const;

export type WalletChainChangePendingResult = {
  readonly status: typeof WALLET_CHAIN_CHANGE_PENDING;
};

export type WalletChainChangeResult =
  | boolean
  | WalletChainChangePendingResult
  | undefined;

export const WALLET_CHAIN_CHANGE_PENDING_RESULT: WalletChainChangePendingResult =
  Object.freeze({
    status: WALLET_CHAIN_CHANGE_PENDING,
  });

export function isWalletChainChangePendingResult(
  value: unknown,
): value is WalletChainChangePendingResult {
  return (
    typeof value === "object" &&
    value !== null &&
    "status" in value &&
    value.status === WALLET_CHAIN_CHANGE_PENDING
  );
}

export enum WS_WalletStatusEnum {
  NO = "NO",
  FAILED = "FAILED",
  PENDING = "PENDING",
  PROCESSING = "PROCESSING",
  COMPLETED = "COMPLETED",
}
