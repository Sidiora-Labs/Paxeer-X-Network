export const PRIVY_OAUTH_CONNECT_INTENT_KEY =
  "paxeer-network:privy-oauth-connect-intent";

const redirectLoginMethods = new Set(["google", "twitter"]);
const OAUTH_CONNECT_INTENT_TTL_MS = 10 * 60 * 1000;

const removeStoredOAuthConnectIntent = () => {
  try {
    window.sessionStorage.removeItem(PRIVY_OAUTH_CONNECT_INTENT_KEY);
  } catch {
    // Storage restrictions must not affect the connection lifecycle.
  }
};

export type OAuthConnectIntent = {
  id: string;
  loginMethod: string;
  phase: "redirecting" | "returned";
  expiresAt: number;
};

export const isRedirectLoginMethod = (loginMethod?: string) =>
  typeof loginMethod === "string" && redirectLoginMethods.has(loginMethod);

const createIntentId = () => {
  if (typeof crypto !== "undefined" && crypto.randomUUID) {
    return crypto.randomUUID();
  }
  return `${Date.now()}-${Math.random().toString(36).slice(2)}`;
};

export const getOAuthConnectIntent = (): OAuthConnectIntent | null => {
  if (typeof window === "undefined") {
    return null;
  }

  try {
    const value = window.sessionStorage.getItem(PRIVY_OAUTH_CONNECT_INTENT_KEY);
    if (!value) {
      return null;
    }

    const intent = JSON.parse(value) as Partial<OAuthConnectIntent>;
    if (
      typeof intent.id !== "string" ||
      typeof intent.loginMethod !== "string" ||
      typeof intent.expiresAt !== "number" ||
      intent.expiresAt <= Date.now() ||
      (intent.phase !== "redirecting" && intent.phase !== "returned")
    ) {
      clearOAuthConnectIntent();
      return null;
    }
    return intent as OAuthConnectIntent;
  } catch {
    removeStoredOAuthConnectIntent();
    return null;
  }
};

export const markOAuthConnectIntent = (
  loginMethod: string,
): OAuthConnectIntent | null => {
  if (typeof window === "undefined") {
    return null;
  }

  const intent: OAuthConnectIntent = {
    id: createIntentId(),
    loginMethod,
    phase: "redirecting",
    expiresAt: Date.now() + OAUTH_CONNECT_INTENT_TTL_MS,
  };

  try {
    window.sessionStorage.setItem(
      PRIVY_OAUTH_CONNECT_INTENT_KEY,
      JSON.stringify(intent),
    );
    return intent;
  } catch {
    // Storage restrictions must not prevent the login itself.
    return null;
  }
};

export const markOAuthConnectIntentReturned = () => {
  const intent = getOAuthConnectIntent();
  if (!intent) {
    return null;
  }

  const returnedIntent: OAuthConnectIntent = {
    ...intent,
    phase: "returned",
    expiresAt: Date.now() + OAUTH_CONNECT_INTENT_TTL_MS,
  };

  try {
    window.sessionStorage.setItem(
      PRIVY_OAUTH_CONNECT_INTENT_KEY,
      JSON.stringify(returnedIntent),
    );
    return returnedIntent;
  } catch {
    return null;
  }
};

export const clearOAuthConnectIntent = (intentId?: string) => {
  if (typeof window === "undefined") {
    return;
  }

  try {
    if (intentId && getOAuthConnectIntent()?.id !== intentId) {
      return;
    }
    removeStoredOAuthConnectIntent();
  } catch {
    // Storage restrictions must not affect the connection lifecycle.
  }
};
