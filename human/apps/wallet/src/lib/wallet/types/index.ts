/** Public account identity and display metadata. Never secret-bearing. */
export interface WalletAccount {
  id: string;
  kind: 'derived' | 'imported' | 'managed' | 'funded';
  address: string;
  name: string;
  derivationPath: string;
  accountIndex: number;
}

/**
 * Coherent public state returned by the self-custody facade.
 * No vault record, key handle, or secret-bearing account is exposed.
 */
export interface SelfCustodyWalletSnapshot {
  hasWallet: boolean;
  migrationRequired: boolean;
  isLocked: boolean;
  sessionRemaining: number;
  accounts: WalletAccount[];
  activeAccount: WalletAccount | null;
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
 * Configuration passed to the PaxeerWallet facade.
 */
export interface PaxeerWalletConfig {
  /** JSON-RPC endpoint for chain 125. */
  rpcUrl: string;
  /** Chain ID. Defaults to 125. */
  chainId?: number;
  /** Session timeout in milliseconds. Defaults to 15 minutes. */
  sessionTimeoutMs?: number;
  /** @deprecated v2 uses one authoritative session timeout. */
  encryptionTimeoutMs?: number;
  /** @deprecated v2 password work factors are stored in authenticated key slots. */
  pinIterations?: number;
  /** Default gas price in wei for native transfers. Defaults to 5n. */
  transferGasPrice?: bigint;
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
