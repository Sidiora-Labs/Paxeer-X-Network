import type { WalletState } from "@paxeer-network/hooks";
import type { WalletConnectType } from "./types";

export const WALLET_CONNECT_PROVIDER_START = "wallet:connect-provider-start";
export const WALLET_CONNECT_PROVIDER_CANCEL = "wallet:connect-provider-cancel";
export const WALLET_CONNECT_ERROR = "wallet:connect-error";
export const WALLET_CONNECT_WALLET_SELECTED = "wallet:connect-wallet-selected";
export const WALLET_CONNECT_OAUTH_RETURNED = "wallet:connect-oauth-returned";
export const WALLET_CONNECT_OAUTH_RESUME = "wallet:connect-oauth-resume";

export type WalletConnectProviderPayload = {
  walletType: WalletConnectType;
  previousConnectorKey?: string;
  previousChainId?: number;
};

export type WalletConnectErrorPayload = WalletConnectProviderPayload & {
  message: string;
};

export type WalletConnectOAuthReturnedPayload = {
  intentId: string;
};

export type WalletConnectOAuthResumePayload =
  WalletConnectOAuthReturnedPayload & {
    wallet: WalletState;
  };

const cancellationMessages = [
  "user rejected",
  "request rejected",
  "user denied",
  "request denied",
  "user cancelled",
  "user canceled",
  "request cancelled",
  "request canceled",
  "connection cancelled",
  "connection canceled",
  "modal closed",
  "closed modal",
  "closed by user",
  "user closed",
  "window closed",
  "wallet not connected",
  "exited_auth_flow",
  "user exited",
];

export const isWalletConnectCancellation = (error: unknown): boolean => {
  let current = error;

  for (let depth = 0; depth < 4; depth += 1) {
    if (typeof current === "string") {
      const description = current.toLowerCase();
      return cancellationMessages.some((message) =>
        description.includes(message),
      );
    }

    if (!current || typeof current !== "object") {
      return false;
    }

    const value = current as {
      code?: unknown;
      name?: unknown;
      message?: unknown;
      shortMessage?: unknown;
      cause?: unknown;
    };

    if (value.code === 4001 || value.code === "4001") {
      return true;
    }

    const description = [value.name, value.message, value.shortMessage]
      .filter((item): item is string => typeof item === "string")
      .join(" ")
      .toLowerCase();

    if (
      description.includes("userrejectedrequesterror") ||
      cancellationMessages.some((message) => description.includes(message))
    ) {
      return true;
    }

    current = value.cause;
  }

  return false;
};

export const getWalletConnectErrorMessage = (
  error: unknown,
  fallback: string,
) => {
  if (error && typeof error === "object") {
    const value = error as { message?: unknown; shortMessage?: unknown };
    if (typeof value.shortMessage === "string" && value.shortMessage) {
      return value.shortMessage;
    }
    if (typeof value.message === "string" && value.message) {
      return value.message;
    }
  }

  if (typeof error === "string" && error) {
    return error;
  }

  return fallback;
};
