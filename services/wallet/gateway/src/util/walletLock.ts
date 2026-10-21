import { sharedNonceStore, type NonceLease, type NonceStore } from '../nonce/store.js';

const tails = new Map<string, Promise<unknown>>();

export async function withWalletLock<T>(
  key: string,
  fn: (lease: NonceLease) => Promise<T>,
  store: NonceStore = sharedNonceStore(),
  ownerActionId?: string,
): Promise<T> {
  const k = key.toLowerCase();
  const locked = (): Promise<T> => store.withLock(k, fn, ownerActionId);
  const prev = tails.get(k) ?? Promise.resolve();
  const next = prev.then(locked, locked);
  const swallowed = next.then(
    () => undefined,
    () => undefined,
  );
  tails.set(k, swallowed);
  try {
    return await next;
  } finally {
    if (tails.get(k) === swallowed) tails.delete(k);
  }
}

export function _walletLockSize(): number {
  return tails.size;
}
