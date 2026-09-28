import { sharedNonceStore, type NonceStore } from '../../nonce/store.js';
import { withWalletLock } from '../../util/walletLock.js';

export async function withDistributedWalletLock<T>(
  address: string,
  fn: () => Promise<T>,
  store: NonceStore = sharedNonceStore(),
): Promise<T> {
  return withWalletLock(address, fn, store);
}
