import { sharedNonceStore, type NonceStore } from '../nonce/store.js';

const tails = new Map<string, Promise<unknown>>();

type Outcome<T> = { ok: true; value: T } | { ok: false; error: unknown };

export async function withWalletLock<T>(
  key: string,
  fn: () => Promise<T>,
  store: NonceStore = sharedNonceStore(),
  ownerActionId?: string,
): Promise<T> {
  const k = key.toLowerCase();
  const locked = async (): Promise<T> => {
    const outcome = await store.withLock<Outcome<T>>(k, async (lease) => {
      try {
        return { ok: true, value: await fn() };
      } catch (error) {
        return { ok: false, error };
      } finally {
        await lease.markForReconcile();
      }
    }, ownerActionId);
    if (!outcome.ok) throw outcome.error;
    return outcome.value;
  };
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
