/** Blockscout API v2 — Transaction types (wallet-relevant subset) */

import type {
  FullHash,
  IntegerString,
  FloatString,
  Timestamp,
  HexString,
  Fee,
  DecodedInput,
} from './common';
import type { Address } from './address';
import type { TokenTransfer } from './token';

// ── Transaction type tags ───────────────────────────────────────────────────

export type TransactionTypeTag =
  | 'coin_transfer'
  | 'contract_call'
  | 'contract_creation'
  | 'rootstock_bridge'
  | 'rootstock_remasc'
  | 'token_creation'
  | 'token_transfer'
  | 'blob_transaction'
  | 'set_code_transaction';

// ── Transaction action ──────────────────────────────────────────────────────

export interface TransactionAction {
  protocol: string;
  type: string;
  data: Record<string, unknown>;
}

// ── Signed authorization (EIP-7702) ─────────────────────────────────────────

export interface SignedAuthorization {
  address_hash: string;
  chain_id: number;
  nonce: IntegerString;
  r: IntegerString;
  s: IntegerString;
  v: number;
  authority: string;
}

// ── Transaction (GET /addresses/:hash/transactions, GET /transactions/:hash) ─

export interface Transaction {
  hash: FullHash;
  result: string;
  status: 'ok' | 'error' | null;
  block_number: number | null;
  timestamp: Timestamp | null;
  from: Address;
  to: Address;
  created_contract: Address | null;
  confirmations: number;
  confirmation_duration: number[];
  value: IntegerString;
  fee: Fee;
  gas_price: IntegerString | null;
  type: number | null;
  gas_used: IntegerString | null;
  gas_limit: IntegerString;
  max_fee_per_gas: IntegerString | null;
  max_priority_fee_per_gas: IntegerString | null;
  base_fee_per_gas: IntegerString | null;
  priority_fee: IntegerString | null;
  transaction_burnt_fee: IntegerString | null;
  nonce: number;
  position: number | null;
  revert_reason: DecodedInput | { raw: string | null } | null;
  raw_input: HexString;
  decoded_input: DecodedInput | null;
  token_transfers: TokenTransfer[] | null;
  token_transfers_overflow: boolean | null;
  actions: TransactionAction[] | null;
  exchange_rate: FloatString | null;
  historic_exchange_rate: FloatString | null;
  method: string | null;
  transaction_types: TransactionTypeTag[];
  transaction_tag: string | null;
  has_error_in_internal_transactions: boolean | null;
  authorization_list: SignedAuthorization[] | null;
}
