/** Blockscout API v2 — Block type (for stats / main-page endpoints) */

import type {
  FullHash,
  IntegerString,
  HexString,
  Timestamp,
} from './common';
import type { Address } from './address';

export interface BlockReward {
  type: string;
  reward: IntegerString;
}

export interface Block {
  height: number;
  timestamp: Timestamp;
  transactions_count: number;
  internal_transactions_count: number | null;
  miner: Address;
  size: number;
  hash: FullHash;
  parent_hash: FullHash;
  difficulty: IntegerString;
  total_difficulty: IntegerString;
  gas_used: IntegerString;
  gas_limit: IntegerString;
  nonce: HexString | null;
  base_fee_per_gas: IntegerString | null;
  burnt_fees: IntegerString | null;
  priority_fee: IntegerString | null;
  uncles_hashes: { hash: FullHash }[];
  rewards: BlockReward[];
  gas_target_percentage: number;
  gas_used_percentage: number;
  burnt_fees_percentage: number | null;
  type: 'block' | 'uncle' | 'reorg';
  transaction_fees: IntegerString;
  withdrawals_count: number | null;
}
