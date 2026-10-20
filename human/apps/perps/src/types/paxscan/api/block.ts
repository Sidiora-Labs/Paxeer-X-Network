// @coord dropped ./arbitrumL2 (ArbitrumBatchStatus, ArbitrumL2TxData) — out of trading-app scope
// @coord dropped ./optimisticL2 (OptimisticL2BatchDataContainer, OptimisticL2BlobTypeEip4844, OptimisticL2BlobTypeCelestia) — out of trading-app scope
// @coord dropped ./zkSyncL2 (ZkSyncBatchesItem) — out of trading-app scope
// @coord dropped local interfaces ArbitrumBlockData / OptimismBlockData — all consumed only by chain-specific fields narrowed to `unknown` below.
import type { AddressParam } from './addressParams';
import type { InternalTransaction } from './internalTransaction';
import type { Reward } from './reward';
import type { TokenInfo } from './token';
import type { Transaction } from './transaction';

export type BlockType = 'block' | 'reorg' | 'uncle';

export interface BlockBaseFeeCelo {
  amount: string;
  breakdown: Array<{ amount: string; percentage: number; address: AddressParam }>;
  recipient: AddressParam;
  token: TokenInfo;
}

export interface Block {
  height: number;
  timestamp: string;
  transactions_count: number;
  internal_transactions_count: number;
  miner: AddressParam;
  size?: number;
  hash: string;
  parent_hash: string;
  difficulty?: string;
  total_difficulty?: string | null;
  gas_used: string | null;
  gas_limit: string;
  nonce: string;
  base_fee_per_gas?: string | null;
  burnt_fees: string | null;
  priority_fee: string | null;
  extra_data: string | null;
  state_root: string | null;
  rewards?: Array<Reward>;
  gas_target_percentage: number | null;
  gas_used_percentage: number | null;
  burnt_fees_percentage: number | null;
  type: BlockType;
  transaction_fees: string | null;
  uncles_hashes: Array<string>;
  withdrawals_count?: number;
  beacon_deposits_count?: number;
  is_pending_update?: boolean;
  // ROOTSTOCK FIELDS
  bitcoin_merged_mining_coinbase_transaction?: string | null;
  bitcoin_merged_mining_header?: string | null;
  bitcoin_merged_mining_merkle_proof?: string | null;
  hash_for_merged_mining?: string | null;
  minimum_gas_price?: string | null;
  // BLOB FIELDS
  blob_gas_price?: string;
  blob_gas_used?: string;
  burnt_blob_fees?: string;
  excess_blob_gas?: string;
  blob_transactions_count?: number;
  // ZKSYNC FIELDS
  // @coord narrowed: was zkSync batch-extended type — out of trading-app scope
  zksync?: unknown;
  // @coord narrowed: was ArbitrumBlockData — out of trading-app scope
  arbitrum?: unknown;
  // @coord narrowed: was OptimismBlockData — out of trading-app scope
  optimism?: unknown;
  // CELO FIELDS
  celo?: {
    epoch_number: number;
    l1_era_finalized_epoch_number: number | null;
    base_fee?: BlockBaseFeeCelo;
  };
  // ZILLIQA FIELDS
  zilliqa?: ZilliqaBlockData;
}

export interface ZilliqaBlockData {
  view: number;
  quorum_certificate: ZilliqaQuorumCertificate;
  aggregate_quorum_certificate: (ZilliqaQuorumCertificate & {
    nested_quorum_certificates: Array<ZilliqaNestedQuorumCertificate>;
  }) | null;
}

export interface ZilliqaQuorumCertificate {
  view: number;
  signature: string;
  signers: Array<number>;
}

export interface ZilliqaNestedQuorumCertificate extends ZilliqaQuorumCertificate {
  proposed_by_validator_index: number;
}

export interface BlocksResponse {
  items: Array<Block>;
  next_page_params: {
    block_number: number;
    items_count: number;
  } | null;
}

export interface BlockTransactionsResponse {
  items: Array<Transaction>;
  next_page_params: {
    block_number: number;
    items_count: number;
    index: number;
  } | null;
}

export interface BlockInternalTransactionsResponse {
  items: Array<InternalTransaction>;
  next_page_params: {
    block_index: number;
    items_count: number;
  } | null;
}

export interface NewBlockSocketResponse {
  average_block_time: string;
  block: Block;
}

export interface BlockFilters {
  type?: BlockType;
}

export type BlockWithdrawalsResponse = {
  items: Array<BlockWithdrawalsItem>;
  next_page_params: {
    index: number;
    items_count: number;
  } | null;
};

export type BlockWithdrawalsItem = {
  amount: string;
  index: number;
  receiver: AddressParam;
  validator_index: number;
};

export interface BlockCountdownResponse {
  result: {
    CountdownBlock: string;
    CurrentBlock: string;
    EstimateTimeInSec: string;
    RemainingBlock: string;
  } | null;
}
