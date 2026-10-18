/**
 * Vendored from `@paxeer/wallet` v0.1.0 (paxeer-embedded-wallet/packages/sdk).
 * paxeer-network-UI uses npm, not pnpm, so we keep a local copy of the SDK source
 * rather than a `file:` workspace link. Update by re-vendoring whenever the
 * upstream SDK ships a new version.
 *
 * Shared SDK types — kept dependency-light so the SDK works in any JS env.
 */

export interface PaxeerWalletConfig {
  /** Base URL of the wallet API (e.g. https://connect.paxportwallet.com). */
  apiUrl: string;
  /** Supabase project URL. */
  supabaseUrl: string;
  /** Supabase publishable / anon key. Safe to ship to browsers. */
  supabaseAnonKey: string;
  /** Optional fetch override (useful in tests / SSR). */
  fetch?: typeof fetch;
}

export interface PublicWallet {
  id: string;
  address: `0x${string}`;
  chain_id: number;
  created_at: string;
  last_used_at: string | null;
}

export interface ChainInfo {
  id: number;
  rpc_url: string;
  explorer_url: string | null;
}

export interface TxRequest {
  to?: `0x${string}`;
  /** Decimal string in wei. */
  value?: string | bigint;
  data?: `0x${string}`;
  /** Decimal string. */
  gas?: string | bigint;
  maxFeePerGas?: string | bigint;
  maxPriorityFeePerGas?: string | bigint;
  nonce?: number;
  chainId?: number;
}

export interface SignTxResponse {
  signed_tx: `0x${string}`;
  address: `0x${string}`;
  chain_id: number;
}

export interface SendTxResponse {
  tx_hash: `0x${string}`;
  address: `0x${string}`;
  chain_id: number;
}

export interface SignMessageResponse {
  signature: `0x${string}`;
  address: `0x${string}`;
}

export class PaxeerWalletError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly status?: number,
    public readonly detail?: unknown,
  ) {
    super(message);
    this.name = "PaxeerWalletError";
  }
}
