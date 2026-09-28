/** Public account identity and display metadata. Never secret-bearing. */
export interface WalletAccount {
  id: string;
  kind: 'managed' | 'injected';
  address: string;
  name: string;
  derivationPath: string;
  accountIndex: number;
}

/**
 * Data needed to build and send a transaction.
 */
export interface TransactionData {
  to: string;
  value: string;
  gasLimit?: string;
  tokenAddress?: string;
  decimals?: number;
  nonce?: number;
}

/**
 * Events emitted by the wallet core.
 */
export const WalletEvents = {
  SESSION_EXPIRED: 'session:expired',
  SESSION_CREATED: 'session:created',
  MANUAL_LOCK: 'wallet:locked',
  ACCOUNT_CHANGED: 'account:changed',
  WALLET_CLEARED: 'wallet:cleared',
} as const;
