/** Blockscout API v2 — Address types (wallet-relevant subset) */

import type {
  AddressHash,
  FullHash,
  IntegerString,
  FloatString,
  Timestamp,
  Tag,
  WatchlistName,
  Metadata,
  Implementation,
  ProxyType,
} from './common';
import type { Token } from './token';

// ── Base address (embedded in tx / transfer responses) ──────────────────────

export interface Address {
  hash: AddressHash;
  is_contract: boolean | null;
  is_scam: boolean;
  is_verified: boolean;
  name: string | null;
  ens_domain_name: string | null;
  metadata: Metadata | null;
  proxy_type: ProxyType;
  implementations: Implementation[];
  private_tags: Tag[];
  public_tags: Tag[];
  watchlist_names: WatchlistName[];
}

// ── Full address response (GET /addresses/:hash) ────────────────────────────

export interface AddressResponse extends Address {
  coin_balance: IntegerString | null;
  exchange_rate: FloatString | null;
  block_number_balance_updated_at: number | null;
  creation_transaction_hash: FullHash | null;
  creator_address_hash: AddressHash | null;
  creation_status: 'success' | 'failed' | 'selfdestructed' | null;
  token: Token | null;
  has_validated_blocks: boolean;
  has_logs: boolean;
  has_tokens: boolean;
  has_token_transfers: boolean;
  has_beacon_chain_withdrawals: boolean;
  watchlist_address_id: number | null;
}

// ── Counters (GET /addresses/:hash/counters) ────────────────────────────────

export interface AddressCounters {
  transactions_count: IntegerString;
  token_transfers_count: IntegerString;
  gas_usage_count: IntegerString;
  validations_count: IntegerString;
}

// ── Tabs counters (GET /addresses/:hash/tabs-counters) ──────────────────────

export interface AddressTabsCounters {
  transactions_count: number;
  token_transfers_count: number;
  token_balances_count: number;
  logs_count: number;
  internal_transactions_count: number;
  validations_count: number;
  withdrawals_count: number;
  celo_election_rewards_count: number;
}

// ── Coin balance history (GET /addresses/:hash/coin-balance-history) ────────

export interface CoinBalance {
  transaction_hash: FullHash | null;
  block_number: number;
  block_timestamp: Timestamp;
  delta: IntegerString;
  value: IntegerString;
}

// ── Coin balance by day (GET /addresses/:hash/coin-balance-history-by-day) ──

export interface CoinBalanceByDay {
  date: string;
  value: IntegerString;
}

export interface CoinBalanceHistoryByDay {
  days: number;
  items: CoinBalanceByDay[];
}
