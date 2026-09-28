/** Blockscout API v2 — Token types (wallet-relevant subset) */

import type {
  AddressHash,
  FullHash,
  IntegerString,
  FloatString,
  Timestamp,
  TokenType,
  HexString,
} from './common';
import type { Address } from './address';

// ── Token (embedded in balances, transfers, etc.) ───────────────────────────

export interface Token {
  address_hash: AddressHash;
  name: string;
  symbol: string;
  decimals: IntegerString | null;
  type: TokenType | null;
  holders_count: IntegerString | null;
  exchange_rate: FloatString | null;
  volume_24h: FloatString | null;
  total_supply: IntegerString | null;
  icon_url: string | null;
  circulating_market_cap: FloatString | null;
}

// ── Token balance (GET /addresses/:hash/tokens or /token-balances) ──────────

export interface TokenBalance {
  value: IntegerString;
  token: Token | null;
  token_id: IntegerString | null;
  token_instance: TokenInstance | null;
}

// ── Token instance (NFTs) ───────────────────────────────────────────────────

export interface TokenInstanceThumbnails {
  original: string;
  '60x60'?: string;
  '250x250'?: string;
  '500x500'?: string;
}

export interface TokenInstance {
  id: IntegerString;
  metadata: Record<string, unknown> | null;
  owner: Address | null;
  token: Token | null;
  external_app_url: string | null;
  animation_url: string | null;
  image_url: string | null;
  is_unique: boolean | null;
  thumbnails: TokenInstanceThumbnails | null;
  media_type: string | null;
  media_url: string | null;
}

// ── Token instance in list (with type + value) ──────────────────────────────

export interface TokenInstanceInList extends TokenInstance {
  token_type: TokenType;
  value: IntegerString | null;
}

// ── NFT collection (GET /addresses/:hash/nft/collections) ───────────────────

export interface NFTCollection {
  token: Token;
  amount: IntegerString | null;
  token_instances: Array<TokenInstance & { token_type: TokenType; value: IntegerString | null }>;
}

// ── Token transfer ──────────────────────────────────────────────────────────

export interface TokenTransferTotal {
  value: IntegerString | null;
  decimals: IntegerString | null;
}

export interface TokenTransferTotalERC721 {
  token_id: IntegerString | null;
  token_instance: TokenInstance | null;
}

export interface TokenTransferTotalERC1155 {
  token_id: IntegerString | null;
  value: IntegerString | null;
  decimals: IntegerString | null;
  token_instance: TokenInstance | null;
}

export type TokenTransferTotalUnion =
  | TokenTransferTotal
  | TokenTransferTotalERC721
  | TokenTransferTotalERC1155;

export interface TokenTransfer {
  transaction_hash: FullHash;
  from: Address;
  to: Address;
  total: TokenTransferTotalUnion | null;
  token: Token;
  type: 'token_burning' | 'token_minting' | 'token_spawning' | 'token_transfer';
  timestamp: Timestamp | null;
  method: string | null;
  block_hash: FullHash;
  block_number: number;
  log_index: number;
}

// ── Log ─────────────────────────────────────────────────────────────────────

export interface Log {
  transaction_hash: FullHash;
  address: Address;
  topics: (HexString | null)[];
  data: HexString;
  index: number;
  decoded: import('./common').DecodedInput | null;
  smart_contract: Address | null;
  block_hash: FullHash;
  block_number: number;
}
