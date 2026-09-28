/** Shared SDK types — kept dependency-light so the SDK works in any JS env. */

export interface PaxeerWalletConfig {
  /** Base URL of the wallet API (e.g. https://wallet.example). */
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

/* ============================================================================
 * Shared endpoint, kernel and human service shapes
 * ========================================================================== */

export type JsonRpcParams = readonly unknown[];

export interface JsonRpcCall {
  method: string;
  params: JsonRpcParams;
}

export interface JsonRpcErrorObject {
  code: number;
  message: string;
  data?: unknown;
}

export type JsonRpcOutcome<T = unknown> =
  | { ok: true; result: T }
  | { ok: false; error: JsonRpcErrorObject };

export interface UnifiedAccountDocument {
  evm_address: `0x${string}` | null;
  pax_address: string | null;
  layerx_did: string | null;
  layerx_account: string | null;
  bound: boolean;
}

export interface PaxeerAccountHalf {
  address: `0x${string}`;
  balance: `0x${string}`;
  nonce: `0x${string}`;
}

export interface JoinedAccount {
  account: UnifiedAccountDocument;
  paxeer: PaxeerAccountHalf | null;
  layerx: Record<string, unknown> | null;
}

export interface CustodyAssetRecord {
  asset_id: string;
  denom: string;
  pointer: `0x${string}`;
  enabled: boolean;
  paused: boolean;
  minimum_deposit: string;
  custody_cap: string;
  custodied: string;
  released: string;
  pending: string;
}

export interface JoinedAsset {
  asset_id: string;
  layerx: Record<string, unknown>;
  paxeer: CustodyAssetRecord | null;
}

export interface AssetMap {
  assets: JoinedAsset[];
  joined_limit: number;
}

export interface JoinedBalanceRow {
  asset_id: string;
  denom: string | null;
  custody: CustodyAssetRecord | null;
  paxeer: { denom: string; amount: string } | null;
  layerx: Record<string, unknown> | null;
}

export interface JoinedBalances {
  account: UnifiedAccountDocument;
  balances: JoinedBalanceRow[];
  joined_limit: number;
}

export type CompletionAsset =
  | { kind: 'native'; symbol: string; decimals: number; denom?: string }
  | { kind: 'erc20'; address: `0x${string}`; symbol?: string; decimals?: number };

export interface CompletedBalance {
  asset: CompletionAsset;
  source: 'eth_getBalance' | 'erc20_balanceOf';
  amount: string | null;
  error: JsonRpcErrorObject | null;
}

export interface AccountBalances extends JoinedBalances {
  asset_map: AssetMap;
  join_limit_reached: boolean;
  completed: CompletedBalance[];
}

export interface Capabilities {
  exchange: boolean;
  bridge: boolean;
  launchpad: boolean;
  probed_at: number;
  rpc_height: string;
}

export type KernelReason = 'available' | 'not_configured' | 'unreachable' | 'no_finalised_checkpoint';

export interface KernelStatus {
  available: boolean;
  reason: KernelReason;
}

export interface AnchorHead {
  latest_finalized_batch: number | null;
  status: number | null;
  status_name: AnchorStatusName | null;
  status_ladder: Record<string, string>;
}

export type AnchorStatusName = 'unknown' | 'submitted' | 'final';

export interface NetworkHead {
  network_id: string;
  paxeer: { chain_id: `0x${string}`; latest_block: `0x${string}` };
  layerx: { node_info: Record<string, unknown> | null };
  anchor: AnchorHead | null;
  kernel: KernelStatus;
}

export interface HistoryAssetMetadata {
  asset: string;
  chain: 'layerx' | 'paxeer';
  kind: string;
  address: string | null;
  denom: string | null;
  symbol: string | null;
  decimals: number | null;
  native_id: string | null;
  pointer: string | null;
  metadata: unknown;
}

export interface HistoryItem {
  id: string;
  height_or_seq: string;
  chain: 'layerx' | 'paxeer';
  kind: string;
  direction: 'in' | 'out';
  account: string;
  counterparty: string | null;
  asset: string;
  amount: string;
  tx_id: string;
  ordinal: string;
  final: boolean;
  decoded: unknown;
  asset_metadata: HistoryAssetMetadata | null;
  side: 'layerx' | 'paxeer';
}

export interface HistoryPage {
  account: UnifiedAccountDocument;
  accounts: { side: 'layerx' | 'paxeer'; account: string }[];
  items: HistoryItem[];
  next_cursor: string | null;
}

export interface HistoryQuery {
  limit?: number;
  kind?: string;
}

export type KernelBackendName =
  | 'core_agent_boundary'
  | 'public_core'
  | 'independent_receipt_authority'
  | 'identity'
  | 'program_registry';

export interface KernelAvailable {
  available: true;
  reason: 'available';
}

export interface KernelUnavailableState {
  available: false;
  reason: Exclude<KernelReason, 'available'>;
  backend: KernelBackendName | null;
}

export type KernelAvailabilityState = KernelAvailable | KernelUnavailableState;

export type KernelRead<T> = { available: true; result: T } | KernelUnavailableState;

export type KernelDocument = Record<string, unknown>;

export interface HumanMoney {
  amount: string;
  currency: string;
}

export type IntentEndpointKind = 'paxeer-wallet' | 'human' | 'agent' | 'agent-budget';

export interface IntentEndpoint {
  kind: IntentEndpointKind;
  account?: string;
}

export type IntentDomain = 'paxeer' | 'layerx';

export interface PlanIntentRequest {
  source: IntentEndpoint;
  destination: IntentEndpoint;
  asset_id: string;
  money: HumanMoney;
  constraints: { deadline: string; max_fee: HumanMoney; allow_top_up: boolean };
}

export interface IntentLeg {
  index: number;
  mechanism: string;
  domain: IntentDomain;
  source: IntentEndpoint;
  destination: IntentEndpoint;
  money: HumanMoney;
  fee: HumanMoney;
}

export interface IntentSigningRequirement {
  leg_index: number;
  action_key: string;
  signing_context: string;
  authority: string;
}

export interface IntentPlan {
  plan_digest: string;
  journey_kind: string;
  total_fee: HumanMoney;
  legs: IntentLeg[];
  signing_requirements: IntentSigningRequirement[];
}

export interface IntentLegBinding {
  leg_index: number;
  action_key: string;
  actor: string;
  authority: string;
  relationship: string;
  account_sequence: number;
  not_before: number;
  not_after: number;
  fee_limit: HumanMoney;
}

export interface SubmitPlanRequest {
  plan_digest: string;
  signed_digest: string;
  bindings: IntentLegBinding[];
}

export type JourneyState =
  | 'getting-ready'
  | 'sending'
  | 'processing'
  | 'done'
  | 'done-finalised'
  | 'still-checking'
  | 'refused'
  | 'waiting-for-you';

export interface IntentSubmission {
  journey_id: string;
  plan_digest: string;
  state: JourneyState;
  state_copy_key: string;
}

export type EvidenceClass =
  | 'local-journey-state'
  | 'submission-record'
  | 'layerx-receipt'
  | 'checkpoint-proof'
  | 'paxeer-finality'
  | 'typed-refusal'
  | 'approval-hold'
  | 'wallet-ack';

export type VerificationLevel = 'unverified' | 'receipt-verified' | 'checkpoint-finalised' | 'paxeer-finalised';

export interface EvidenceRef {
  evidence_id: string;
  class: EvidenceClass;
  verification: VerificationLevel;
  settlement_domain?: string;
}

export interface JourneyStage {
  stage_id: string;
  copy_key: string;
  state: JourneyState;
  evidence: EvidenceRef[];
}

export type JourneyKind =
  | 'onboarding'
  | 'wallet-binding'
  | 'deposit'
  | 'withdraw'
  | 'exit'
  | 'move'
  | 'agent-create'
  | 'agent-fund'
  | 'agent-pause'
  | 'agent-retire';

export interface Journey {
  journey_id: string;
  kind: JourneyKind;
  state: JourneyState;
  state_copy_key: string;
  stages: JourneyStage[];
  evidence: EvidenceRef[];
  started_at: string;
  updated_at: string;
  refusal?: Record<string, unknown>;
  wallet_request?: Record<string, unknown>;
}

export interface HumanErrorBody {
  code: string;
  copy_key: string;
  retry: 'retriable' | 'retriable-after' | 'structural' | 'final';
  retry_after_ms?: number;
  field?: string;
}

export type ExplorerRung = 'pending' | 'instant' | 'sealed' | 'final';

export interface ExplorerTransactionStatus {
  rung: ExplorerRung;
  block_number: number | null;
  sealed_batch_number: number | null;
  finalized_batch_number: number | null;
  checkpoint_id: string | null;
}
