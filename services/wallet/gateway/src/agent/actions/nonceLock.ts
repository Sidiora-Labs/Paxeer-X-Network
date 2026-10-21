import { sharedNonceStore, type NonceLease, type NonceStore } from '../../nonce/store.js';
import { withWalletLock } from '../../util/walletLock.js';

export async function withDistributedWalletLock<T>(
  address: string,
  fn: (lease: NonceLease) => Promise<T>,
  store: NonceStore = sharedNonceStore(),
  ownerActionId?: string,
): Promise<T> {
  return withWalletLock(address, fn, store, ownerActionId);
}
