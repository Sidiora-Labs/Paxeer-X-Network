// @coord dropped ./arbitrumL2 (ArbitrumBatchStatus, ArbitrumL2TxData) — out of trading-app scope
// @coord dropped ./interop (ChainInfo, MessageStatus) — out of trading-app scope
// @coord dropped ./optimisticL2 (OptimisticL2WithdrawalClaimInfo, OptimisticL2WithdrawalStatus) — out of trading-app scope
// @coord dropped ./scrollL2 (ScrollL2BlockStatus) — out of trading-app scope
// @coord dropped ./zkSyncL2 (ZkSyncBatchesItem) — out of trading-app scope
// @coord dropped local interfaces ArbitrumTransactionData / ScrollTransactionData / OpWithdrawal / InteropTransactionInfo and ZKEVM_L2_TX_STATUSES constant — all consumed only by chain-specific fields narrowed to `unknown` below.
import type { AddressParam } from './addressParams';
import type { BlockTransactionsResponse } from './block';
import type { DecodedInput } from './decodedInput';
import type { Fee } from './fee';
import type { TokenInfo } from './token';
import type { TokenTransfer } from './tokenTransfer';
import type { TxAction } from './txAction';

export type TransactionRevertReason = {
  raw: string;
} | DecodedInput;

export type WrappedTransactionFields = 'decoded_input' | 'fee' | 'gas_limit' | 'gas_price' | 'hash' | 'max_fee_per_gas' |
'max_priority_fee_per_gas' | 'method' | 'nonce' | 'raw_input' | 'to' | 'type' | 'value';

export type Transaction = {
  to: AddressParam | null;
  created_contract: AddressParam | null;
  hash: string;
  result: string;
  confirmations: number;
  status: 'ok' | 'error' | null | undefined;
  block_number: number | null;
  timestamp: string | null;
  confirmation_duration: Array<number> | null;
  from: AddressParam;
  value: string;
  fee: Fee;
  gas_price: string | null;
  type: number | null;
  gas_used: string | null;
  gas_limit: string;
  max_fee_per_gas: string | null;
  max_priority_fee_per_gas: string | null;
  priority_fee: string | null;
  base_fee_per_gas: string | null;
  transaction_burnt_fee: string | null;
  nonce: number;
  position: number | null;
  revert_reason: TransactionRevertReason | null;
  raw_input: string;
  decoded_input: DecodedInput | null;
  token_transfers: Array<TokenTransfer> | null;
  token_transfers_overflow: boolean;
  exchange_rate: string | null;
  historic_exchange_rate: string | null;
  method: string | null;
  transaction_types: Array<TransactionType>;
  transaction_tag: string | null;
  actions: Array<TxAction>;
  l1_fee?: string;
  l1_fee_scalar?: string;
  l1_gas_price?: string;
  l1_gas_used?: string;
  has_error_in_internal_transactions: boolean | null;
  is_pending_update?: boolean;
  // optimism fields
  // @coord narrowed: was Array<OpWithdrawal> — out of trading-app scope
  op_withdrawals?: Array<unknown>;
  operator_fee?: string;
  // SUAVE fields
  execution_node?: AddressParam | null;
  allowed_peekers?: Array<string>;
  wrapped?: Pick<Transaction, WrappedTransactionFields>;
  // Stability fields
  stability_fee?: {
    dapp_address: AddressParam;
    dapp_fee: string;
    token: TokenInfo;
    total_fee: string;
    validator_address: AddressParam;
    validator_fee: string;
  };
  // Celo fields
  celo?: {
    gas_token: TokenInfo | null;
  };
  // zkEvm fields
  zkevm_verify_hash?: string;
  zkevm_batch_number?: number;
  // @coord narrowed: was typeof ZKEVM_L2_TX_STATUSES[number] — out of trading-app scope
  zkevm_status?: unknown;
  zkevm_sequence_hash?: string;
  // zkSync FIELDS
  // @coord narrowed: was zkSync batch-extended type — out of trading-app scope
  zksync?: unknown;
  // Zilliqa fields
  zilliqa?: {
    is_scilla: boolean;
  };
  // blob tx fields
  blob_versioned_hashes?: Array<string>;
  blob_gas_used?: string;
  blob_gas_price?: string;
  burnt_blob_fee?: string;
  max_fee_per_blob_gas?: string;
  // @coord narrowed: was ArbitrumTransactionData — out of trading-app scope
  arbitrum?: unknown;
  // @coord narrowed: was ScrollTransactionData — out of trading-app scope
  scroll?: unknown;
  // EIP-7702
  authorization_list?: Array<TxAuthorization>;
  // Interop
  // @coord narrowed: was Array<InteropTransactionInfo> — out of trading-app scope
  op_interop_messages?: Array<unknown>;
  // FHE operations
  fhe_operations_count?: number;
};

export interface TransactionsStats {
  pending_transactions_count: string;
  transaction_fees_avg_24h: string;
  transaction_fees_sum_24h: string;
  transactions_count_24h: string;
}

export type TransactionsResponse = TransactionsResponseValidated | TransactionsResponsePending;

export interface TransactionsResponseValidated {
  items: Array<Transaction>;
  next_page_params: {
    block_number: number;
    index: number;
    items_count: number;
    filter: 'validated';
  } | null;
}

export interface TransactionsResponsePending {
  items: Array<Transaction>;
  next_page_params: {
    inserted_at: string;
    hash: string;
    filter: 'pending';
  } | null;
}

export interface TransactionsResponseWithBlobs {
  items: Array<Transaction>;
  next_page_params: {
    block_number: number;
    index: number;
    items_count: number;
  } | null;
}

export interface TransactionsResponseWatchlist {
  items: Array<Transaction>;
  next_page_params: {
    block_number: number;
    index: number;
    items_count: 50;
  } | null;
}

export type TransactionType = 'rootstock_remasc' |
'rootstock_bridge' |
'token_transfer' |
'contract_creation' |
'contract_call' |
'token_creation' |
'coin_transfer' |
'blob_transaction';

export type TxsResponse = TransactionsResponseValidated | TransactionsResponsePending | BlockTransactionsResponse;

export interface TransactionsSorting {
  sort: 'value' | 'fee' | 'block_number';
  order: 'asc' | 'desc';
}

export type TransactionsSortingField = TransactionsSorting['sort'];

export type TransactionsSortingValue = `${ TransactionsSortingField }-${ TransactionsSorting['order'] }` | 'default';

export interface TxAuthorization {
  address_hash: string;
  authority: string;
  chain_id: number;
  nonce: number;
  status: 'ok' | 'invalid_chain_id' | 'invalid_nonce' | 'invalid_signature' | null;
}
