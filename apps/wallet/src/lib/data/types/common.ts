/** Blockscout API v2 — Common / shared types */

// ── Primitives ──────────────────────────────────────────────────────────────

/** 0x-prefixed 40-hex-char address */
export type AddressHash = string;

/** 0x-prefixed 64-hex-char hash (block / tx) */
export type FullHash = string;

/** 0x-prefixed arbitrary-length hex string */
export type HexString = string;

/** ISO-8601 date-time string */
export type Timestamp = string;

/** Integer serialised as string (e.g. wei values) */
export type IntegerString = string;

/** Float serialised as string (e.g. exchange rates) */
export type FloatString = string;

// ── Pagination ──────────────────────────────────────────────────────────────

export interface PaginatedResponse<T> {
  items: T[];
  next_page_params: Record<string, unknown> | null;
}

export interface PaginationParams {
  items_count?: number;
  [key: string]: unknown;
}

// ── Decoded input ───────────────────────────────────────────────────────────

export interface DecodedInputParameter {
  name: string;
  type: string;
  value: unknown;
}

export interface DecodedInput {
  method_id: string | null;
  method_call: string | null;
  parameters: DecodedInputParameter[];
}

// ── Tags / metadata ─────────────────────────────────────────────────────────

export interface Tag {
  address_hash: AddressHash;
  display_name: string;
  label: string;
}

export interface WatchlistName {
  display_name: string;
  label: string;
}

export interface MetadataTag {
  slug: string;
  name: string;
  tagType: string;
  ordinal: number;
  meta: Record<string, unknown>;
}

export interface Metadata {
  tags: MetadataTag[];
}

// ── Implementation (proxy contracts) ────────────────────────────────────────

export interface Implementation {
  address_hash: AddressHash;
  name: string | null;
}

export type ProxyType =
  | 'eip1167'
  | 'eip1967'
  | 'eip1822'
  | 'eip930'
  | 'master_copy'
  | 'basic_implementation'
  | 'basic_get_implementation'
  | 'comptroller'
  | 'eip2535'
  | 'clone_with_immutable_arguments'
  | 'eip7702'
  | 'resolved_delegate_proxy'
  | 'erc7760'
  | 'unknown'
  | null;

// ── Fee ─────────────────────────────────────────────────────────────────────

export interface Fee {
  type: 'maximum' | 'actual';
  value: IntegerString | null;
}

// ── Token type enum ─────────────────────────────────────────────────────────

export type TokenType = 'ERC-20' | 'ERC-721' | 'ERC-1155' | 'ERC-404';
