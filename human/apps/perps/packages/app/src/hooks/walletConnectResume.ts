import type { WalletState } from "@paxeer-network/hooks";
import type { AccountStatusEnum } from "@paxeer-network/types";

const PRIVY_OAUTH_CONNECT_INTENT_KEY = "paxeer-network:privy-oauth-connect-intent";

export const WALLET_CONNECT_OAUTH_RESUME = "wallet:connect-oauth-resume";
export const WALLET_CONNECT_OAUTH_RETURNED = "wallet:connect-oauth-returned";
export const WALLET_CONNECT_OAUTH_RESUME_RESULT =
  "wallet:connect-oauth-resume-result";

type StoredOAuthConnectIntent = {
  id: string;
  phase: "redirecting" | "returned";
  expiresAt: number;
};

const removeStoredOAuthConnectIntent = () => {
  try {
    window.sessionStorage.removeItem(PRIVY_OAUTH_CONNECT_INTENT_KEY);
  } catch {
    // Storage restrictions must not affect account validation.
  }
};

export type WalletConnectOAuthResumePayload = {
  intentId: string;
  wallet: WalletState;
};

export type WalletConnectOAuthReturnedPayload = {
  intentId: string;
};

export type WalletConnectOAuthResumeResult = {
  intentId: string;
  status?: AccountStatusEnum;
  wrongNetwork?: boolean;
  handled?: boolean;
};

const getStoredIntent = (): StoredOAuthConnectIntent | null => {
  if (typeof window === "undefined") {
    return null;
  }

  try {
    const value = window.sessionStorage.getItem(PRIVY_OAUTH_CONNECT_INTENT_KEY);
    if (!value) {
      return null;
    }
    const intent = JSON.parse(value) as Partial<StoredOAuthConnectIntent>;
    if (
      typeof intent.id !== "string" ||
      typeof intent.expiresAt !== "number" ||
      intent.expiresAt <= Date.now() ||
      (intent.phase !== "redirecting" && intent.phase !== "returned")
    ) {
      removeStoredOAuthConnectIntent();
      return null;
    }
    return intent as StoredOAuthConnectIntent;
  } catch {
    removeStoredOAuthConnectIntent();
    return null;
  }
};

export const hasOAuthConnectIntent = () => getStoredIntent() !== null;

export const getReturnedOAuthConnectIntent = () => {
  const intent = getStoredIntent();
  return intent?.phase === "returned" ? intent : null;
};

export const matchesReturnedOAuthConnectIntent = (intentId: string) => {
  const intent = getStoredIntent();
  return intent?.id === intentId && intent.phase === "returned";
};

export const clearOAuthConnectIntent = (intentId: string) => {
  if (typeof window === "undefined") {
    return;
  }

  try {
    if (getStoredIntent()?.id === intentId) {
      removeStoredOAuthConnectIntent();
    }
  } catch {
    // Storage restrictions must not affect account validation.
  }
};
