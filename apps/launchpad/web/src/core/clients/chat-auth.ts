// Shared chat auth — sign with nonce + expiration, auto-rotate.
// Persisted in sessionStorage with expiration-based invalidation.
//
// BACKEND COMPATIBILITY NOTE:
// The old message format was: "Sidiora Auth <wallet>"
// The new format is: "Sidiora Auth\nWallet: <wallet>\nNonce: <nonce>\nExpires At: <iso>"
// The backend MUST be updated to verify the new format. Until then, both the
// old headers (x-wallet, x-signature, x-message) and new fields (nonce, expiresAt)
// are sent so the backend can be migrated incrementally.

const STORAGE_KEY = 'sidiora_chat_auth';
const AUTH_TTL_MS = 10 * 60 * 1000; // 10 minutes

export interface ChatAuth {
  wallet: string;
  signature: string;
  message: string;
}

export interface ChatAuthPayload {
  wallet: string;
  message: string;
  signature: string;
  nonce: string;
  expiresAt: string;
}

interface StoredAuth extends ChatAuthPayload {
  createdAt: number;
}

let cached: StoredAuth | null = null;
let pendingPromise: Promise<ChatAuthPayload | null> | null = null;

function generateNonce(): string {
  const arr = new Uint8Array(16);
  crypto.getRandomValues(arr);
  return Array.from(arr, (b) => b.toString(16).padStart(2, '0')).join('');
}

function isExpired(auth: StoredAuth): boolean {
  return Date.now() >= new Date(auth.expiresAt).getTime();
}

function loadFromStorage(): StoredAuth | null {
  if (typeof window === 'undefined') return null;
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as StoredAuth;
    if (!parsed?.wallet || !parsed?.signature || !parsed?.message || !parsed?.nonce || !parsed?.expiresAt) {
      removeFromStorage();
      return null;
    }
    if (isExpired(parsed)) {
      removeFromStorage();
      return null;
    }
    return parsed;
  } catch {
    removeFromStorage();
    return null;
  }
}

function saveToStorage(auth: StoredAuth) {
  try { sessionStorage.setItem(STORAGE_KEY, JSON.stringify(auth)); } catch {}
}

function removeFromStorage() {
  try { sessionStorage.removeItem(STORAGE_KEY); } catch {}
}

cached = loadFromStorage();

export function getCachedAuth(): ChatAuthPayload | null {
  if (!cached) cached = loadFromStorage();
  if (cached && isExpired(cached)) {
    clearAuth();
    return null;
  }
  return cached ? toPayload(cached) : null;
}

function toPayload(stored: StoredAuth): ChatAuthPayload {
  return {
    wallet: stored.wallet,
    message: stored.message,
    signature: stored.signature,
    nonce: stored.nonce,
    expiresAt: stored.expiresAt,
  };
}

export function clearAuth() {
  cached = null;
  pendingPromise = null;
  removeFromStorage();
}

export async function ensureAuth(
  wallet: string,
  signMessageAsync: (args: { message: string }) => Promise<string>,
): Promise<ChatAuthPayload | null> {
  if (!cached) cached = loadFromStorage();

  if (cached && cached.wallet.toLowerCase() === wallet.toLowerCase() && !isExpired(cached)) {
    return toPayload(cached);
  }

  if (cached && (cached.wallet.toLowerCase() !== wallet.toLowerCase() || isExpired(cached))) {
    clearAuth();
  }

  if (pendingPromise) return pendingPromise;

  pendingPromise = (async () => {
    try {
      const nonce = generateNonce();
      const expiresAt = new Date(Date.now() + AUTH_TTL_MS).toISOString();
      const msg = `Sidiora Auth\nWallet: ${wallet.toLowerCase()}\nNonce: ${nonce}\nExpires At: ${expiresAt}`;
      const sig = await signMessageAsync({ message: msg });
      const stored: StoredAuth = {
        wallet,
        signature: sig,
        message: msg,
        nonce,
        expiresAt,
        createdAt: Date.now(),
      };
      cached = stored;
      saveToStorage(stored);
      return toPayload(stored);
    } catch {
      return null;
    } finally {
      pendingPromise = null;
    }
  })();

  return pendingPromise;
}
