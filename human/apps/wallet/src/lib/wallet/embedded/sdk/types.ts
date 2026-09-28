/**
 * Vendored from `@paxeer/wallet` v0.1.0
 * (`/paxport/paxeer-embedded-wallet/packages/sdk/src/types.ts`).
 *
 * Why vendored: `/paxport/wallet` is a source-only library — it has no
 * package.json — so we can't depend on the SDK via a workspace symlink. We
 * keep a local copy here. Re-vendor whenever the upstream SDK ships a
 * material change to its public surface.
 *
 * Shared SDK types — kept dependency-light so the SDK works in any JS env
 * (browser, RN, Electron, edge runtime). The only external runtime dep is
 * `@supabase/supabase-js`, declared by the consumer of /paxport/wallet.
 */

export interface PaxeerEmbeddedConfig {
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

export type OAuthProvider =
    | 'google'
    | 'apple'
    | 'twitter'
    | 'github'
    | 'discord';

export class PaxeerWalletError extends Error {
    constructor(
        message: string,
        public readonly code: string,
        public readonly status?: number,
        public readonly detail?: unknown,
    ) {
        super(message);
        this.name = 'PaxeerWalletError';
    }
}
/* ============================================================================
 * Funded accounts (prop-firm tier)
 * ========================================================================== */

/** Stable machine-friendly deny codes returned by the funded policy engine. */
export type FundedDenyCode =
  | 'ACCOUNT_BREACHED'
  | 'ACCOUNT_CLOSED'
  | 'WITHDRAWAL_BLOCKED'
  | 'CONTRACT_CREATION_BLOCKED'
  | 'CONTRACT_NOT_WHITELISTED'
  | 'NATIVE_VALUE_NOT_ALLOWED'
  | 'APPROVE_SPENDER_NOT_WHITELISTED'
  | 'INVALID_TX_SHAPE';

/** Funded account state machine, see HANDOFF.md §6.2. */
export type FundedAccountStatus =
  | 'pending_funding'
  | 'active'
  | 'scale_eligible'
  | 'payout_eligible'
  | 'breached_daily'
  | 'breached_max'
  | 'closed';

/** Single (contract, selector) row from the tier whitelist. */
export interface FundedWhitelistEntry {
  contract_address: string;
  /** First 4 bytes of calldata (`0xabcdef12`) or `null` for wildcard match. */
  selector: string | null;
  allow_native_value: boolean;
  label: string;
}

/** Public tier definition exposed by `GET /v1/funded/tiers`. */
export interface FundedTier {
  tier_id: string;
  label: string;
  /** Raw initial USDL collateral, base units (e.g. "25000000000" for 25K @ 6dp). */
  initial_usdl_units: string;
  initial_usdl_decimals: number;
  /** Initial native PAX gas grant in wei. */
  initial_pax_wei: string;
  /** Daily drawdown limit in basis points (1500 = 15%). */
  max_daily_dd_bps: number;
  /** Total drawdown limit in basis points (2500 = 25%). */
  max_total_dd_bps: number;
  /** Equity peak (USD, 6dp) that flips status to scale_eligible. */
  scale_threshold_usd: string;
  /** Equity peak (USD, 6dp) that flips status to payout_eligible. */
  payout_threshold_usd: string;
  /** Profit share taken on payout (1000 = 10%). */
  capital_fee_bps: number;
  whitelist: FundedWhitelistEntry[];
}

/** Funded account row returned by `/v1/funded/me` and `/v1/funded/provision`. */
export interface FundedAccount {
  id: string;
  tier_id: string;
  status: FundedAccountStatus;
  /** USD value (6dp) at provision time. */
  starting_value_usd: string;
  /** Highest equity ever observed (USD, 6dp). Drives milestone transitions. */
  peak_value_usd: string;
  /** Current equity from the latest evaluator tick (USD, 6dp). */
  current_value_usd: string | null;
  /** Equity at the start of the current daily window (USD, 6dp). */
  daily_start_value_usd: string | null;
  /** Timestamp of the last daily window rollover. */
  daily_start_at: string | null;
  /** When the evaluator last touched this row. */
  last_eval_at: string | null;
  /** Disbursement tx hashes — present once funding completes. */
  funding_tx_hashes: { usdl?: string; pax?: string };
  /** Profit share owed once user reaches payout_eligible (USD, 6dp). */
  capital_fee_owed_usd: string;
  /** Free-form description present iff status is breached_*. */
  breached_reason: string | null;
  created_at: string;
}

/** Compact tier params returned alongside `/v1/funded/me`. */
export interface FundedTierSummary {
  tier_id: string;
  label: string;
  max_daily_dd_bps: number;
  max_total_dd_bps: number;
  scale_threshold_usd: string;
  payout_threshold_usd: string;
  capital_fee_bps: number;
}

/** Live on-chain balances for the funded wallet. */
export interface FundedBalances {
  pax_wei: string | null;
  usdl_units: string | null;
  usdl_decimals: number;
}

/** Response from `GET /v1/funded/me`. */
export interface FundedSelfResponse {
  wallet: PublicWallet & { kind: 'funded' };
  funded_account: FundedAccount;
  tier: FundedTierSummary | null;
  balances: FundedBalances;
  whitelist: FundedWhitelistEntry[];
}

/**
 * Disbursement details. Shape varies between fresh provision (returns the
 * full `disburseTier` payload) and idempotent retry (returns the persisted
 * `funding_tx_hashes` blob). Both forms are normalised here.
 */
export interface FundedFundingTxHashes {
  usdl?: string;
  pax?: string;
  usdl_tx_hash?: string;
  pax_tx_hash?: string;
  tier_id?: string;
  usdl_amount?: string;
  pax_amount_wei?: string;
}

/** Response from `POST /v1/funded/provision`. */
export interface FundedProvisionResponse {
  wallet: PublicWallet & { kind: 'funded' };
  funded_account: FundedAccount;
  funding: {
    status: 'funded' | 'already_funded';
    tx_hashes: FundedFundingTxHashes;
  };
}

/** Structured 403 body returned when the funded policy denies a tx. */
export interface FundedDenyDetail {
  error: FundedDenyCode;
  message: string;
  contract?: string;
  selector?: string;
  spender?: string;
  status?: FundedAccountStatus;
}
