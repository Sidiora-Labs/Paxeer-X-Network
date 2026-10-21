/**
 * Paxscan resource catalog.
 *
 * The RESOURCES table below is trimmed to the 24 Blockscout endpoints required by the
 * Paxscan client spec (6 tx + 8 address + 3 block + 3 token + 2 search + 2 homepage).
 * Every `path` / `pathParams` / `filterFields` / `paginated` / `headers` entry is copied
 * verbatim from Blockscout's
 * `/paxscan/frontend-2.7.0-alpha/lib/api/services/general/{tx,address,block,token,misc}.ts`.
 *
 * Keys use paxscan-canonical names per the dispatch spec:
 *   tx*                    → transaction*
 *   address_internal_txs   → address_internal_transactions
 *   address_nfts           → address_nft
 *   stats                  → homepage_stats
 * Everything else matches Blockscout 1:1.
 */

import type {
  Address,
  AddressCounters,
  AddressInternalTxsResponse,
  AddressNFTsResponse,
  AddressTokenTransferResponse,
  AddressTokensResponse,
  AddressTransactionsResponse,
} from '@/types/paxscan/api/address';
import type { Block, BlockTransactionsResponse, BlocksResponse } from '@/types/paxscan/api/block';
import type { IndexingStatus } from '@/types/paxscan/api/indexingStatus';
import type { InternalTransactionsResponse } from '@/types/paxscan/api/internalTransaction';
import type { LogsResponseAddress, LogsResponseTx } from '@/types/paxscan/api/log';
import type { RawTracesResponse } from '@/types/paxscan/api/rawTrace';
import type { SearchRedirectResult, SearchResult } from '@/types/paxscan/api/search';
import type { HomeStats } from '@/types/paxscan/api/stats';
import type { TokenCounters, TokenHolders, TokenInfo } from '@/types/paxscan/api/token';
import type { TokenTransferResponse } from '@/types/paxscan/api/tokenTransfer';
import type { Transaction } from '@/types/paxscan/api/transaction';
import type { TxStateChanges } from '@/types/paxscan/api/txStateChanges';

export interface ApiResource {
  path: string;
  pathParams?: ReadonlyArray<string>;
  filterFields?: ReadonlyArray<string>;
  paginated?: boolean;
  headers?: Record<string, string>;
}

// -----------------------------------------------------------------------------
// RESOURCES table
// -----------------------------------------------------------------------------

export const RESOURCES = {
  // Transactions -------------------------------------------------------------
  'general:transaction': {
    path: '/api/v2/transactions/:hash',
    pathParams: [ 'hash' ] as const,
  },
  'general:transaction_logs': {
    path: '/api/v2/transactions/:hash/logs',
    pathParams: [ 'hash' ] as const,
    filterFields: [] as const,
    paginated: true,
  },
  'general:transaction_token_transfers': {
    path: '/api/v2/transactions/:hash/token-transfers',
    pathParams: [ 'hash' ] as const,
    filterFields: [ 'type' ] as const,
    paginated: true,
  },
  'general:transaction_internal_transactions': {
    path: '/api/v2/transactions/:hash/internal-transactions',
    pathParams: [ 'hash' ] as const,
    filterFields: [] as const,
    paginated: true,
  },
  'general:transaction_state_changes': {
    path: '/api/v2/transactions/:hash/state-changes',
    pathParams: [ 'hash' ] as const,
    filterFields: [] as const,
    paginated: true,
  },
  'general:transaction_raw_trace': {
    path: '/api/v2/transactions/:hash/raw-trace',
    pathParams: [ 'hash' ] as const,
  },

  // Addresses ----------------------------------------------------------------
  'general:address': {
    path: '/api/v2/addresses/:hash',
    pathParams: [ 'hash' ] as const,
  },
  'general:address_txs': {
    path: '/api/v2/addresses/:hash/transactions',
    pathParams: [ 'hash' ] as const,
    filterFields: [ 'filter' ] as const,
    paginated: true,
  },
  'general:address_token_transfers': {
    path: '/api/v2/addresses/:hash/token-transfers',
    pathParams: [ 'hash' ] as const,
    filterFields: [ 'filter', 'type', 'token' ] as const,
    paginated: true,
  },
  'general:address_internal_transactions': {
    path: '/api/v2/addresses/:hash/internal-transactions',
    pathParams: [ 'hash' ] as const,
    filterFields: [ 'filter' ] as const,
    paginated: true,
  },
  'general:address_logs': {
    path: '/api/v2/addresses/:hash/logs',
    pathParams: [ 'hash' ] as const,
    filterFields: [] as const,
    paginated: true,
  },
  'general:address_tokens': {
    path: '/api/v2/addresses/:hash/tokens',
    pathParams: [ 'hash' ] as const,
    filterFields: [ 'type' ] as const,
    paginated: true,
  },
  'general:address_nft': {
    path: '/api/v2/addresses/:hash/nft',
    pathParams: [ 'hash' ] as const,
    filterFields: [ 'type' ] as const,
    paginated: true,
  },
  'general:address_counters': {
    path: '/api/v2/addresses/:hash/counters',
    pathParams: [ 'hash' ] as const,
  },

  // Blocks -------------------------------------------------------------------
  'general:block': {
    path: '/api/v2/blocks/:height_or_hash',
    pathParams: [ 'height_or_hash' ] as const,
  },
  'general:block_txs': {
    path: '/api/v2/blocks/:height_or_hash/transactions',
    pathParams: [ 'height_or_hash' ] as const,
    filterFields: [ 'type' ] as const,
    paginated: true,
  },
  'general:blocks': {
    path: '/api/v2/blocks',
    filterFields: [ 'type' ] as const,
    paginated: true,
  },

  // Tokens -------------------------------------------------------------------
  'general:token': {
    path: '/api/v2/tokens/:hash',
    pathParams: [ 'hash' ] as const,
  },
  'general:token_holders': {
    path: '/api/v2/tokens/:hash/holders',
    pathParams: [ 'hash' ] as const,
    filterFields: [] as const,
    paginated: true,
  },
  'general:token_counters': {
    path: '/api/v2/tokens/:hash/counters',
    pathParams: [ 'hash' ] as const,
  },

  // Search -------------------------------------------------------------------
  'general:search': {
    path: '/api/v2/search',
    filterFields: [ 'q' ] as const,
    paginated: true,
  },
  'general:search_check_redirect': {
    path: '/api/v2/search/check-redirect',
  },

  // Homepage -----------------------------------------------------------------
  'general:homepage_stats': {
    path: '/api/v2/stats',
    headers: {
      'updated-gas-oracle': 'true',
    },
  },
  'general:homepage_indexing_status': {
    path: '/api/v2/main-page/indexing-status',
  },
} satisfies Record<string, ApiResource>;

// -----------------------------------------------------------------------------
// Derived public types
// -----------------------------------------------------------------------------

export type ResourceName = keyof typeof RESOURCES;

export type PaginatedResourceName = {
  [R in ResourceName]: (typeof RESOURCES)[R] extends { paginated: true } ? R : never;
}[ResourceName];

// -----------------------------------------------------------------------------
// PathParams — explicit map keeps pathParam shape readable and type-safe
// without heavy conditional-type machinery.
// -----------------------------------------------------------------------------

type PathParamsMap = {
  'general:transaction': { hash: string };
  'general:transaction_logs': { hash: string };
  'general:transaction_token_transfers': { hash: string };
  'general:transaction_internal_transactions': { hash: string };
  'general:transaction_state_changes': { hash: string };
  'general:transaction_raw_trace': { hash: string };
  'general:address': { hash: string };
  'general:address_txs': { hash: string };
  'general:address_token_transfers': { hash: string };
  'general:address_internal_transactions': { hash: string };
  'general:address_logs': { hash: string };
  'general:address_tokens': { hash: string };
  'general:address_nft': { hash: string };
  'general:address_counters': { hash: string };
  'general:block': { height_or_hash: string };
  'general:block_txs': { height_or_hash: string };
  'general:blocks': Record<string, never>;
  'general:token': { hash: string };
  'general:token_holders': { hash: string };
  'general:token_counters': { hash: string };
  'general:search': Record<string, never>;
  'general:search_check_redirect': Record<string, never>;
  'general:homepage_stats': Record<string, never>;
  'general:homepage_indexing_status': Record<string, never>;
};

export type PathParams<R extends ResourceName> = R extends keyof PathParamsMap ? PathParamsMap[R] : never;

// -----------------------------------------------------------------------------
// QueryParams — deliberately loose in Phase 1. Tightening to per-resource
// filter/sorting unions requires Agent 1 freezing ~a dozen filter types; tracked
// as a future supervisor decision.
// -----------------------------------------------------------------------------

export type QueryParams<_R extends ResourceName = ResourceName> =
  Record<string, string | Array<string> | number | boolean | null | undefined>;

// -----------------------------------------------------------------------------
// ResourcePayload — per-resource response type.
// Each entry maps a paxscan-canonical resource key to the corresponding
// Blockscout payload type ported by Agent 1 (FROZEN in supervisor.ledger.md §1
// Phase-2 batch @ 2026-04-18T08:51:00+02:00).
// -----------------------------------------------------------------------------

type PayloadMap = {
  'general:transaction': Transaction;
  'general:transaction_logs': LogsResponseTx;
  'general:transaction_token_transfers': TokenTransferResponse;
  'general:transaction_internal_transactions': InternalTransactionsResponse;
  'general:transaction_state_changes': TxStateChanges;
  'general:transaction_raw_trace': RawTracesResponse;
  'general:address': Address;
  'general:address_txs': AddressTransactionsResponse;
  'general:address_token_transfers': AddressTokenTransferResponse;
  'general:address_internal_transactions': AddressInternalTxsResponse;
  'general:address_logs': LogsResponseAddress;
  'general:address_tokens': AddressTokensResponse;
  'general:address_nft': AddressNFTsResponse;
  'general:address_counters': AddressCounters;
  'general:block': Block;
  'general:block_txs': BlockTransactionsResponse;
  'general:blocks': BlocksResponse;
  'general:token': TokenInfo;
  'general:token_holders': TokenHolders;
  'general:token_counters': TokenCounters;
  'general:search': SearchResult;
  'general:search_check_redirect': SearchRedirectResult;
  'general:homepage_stats': HomeStats;
  'general:homepage_indexing_status': IndexingStatus;
};

export type ResourcePayload<R extends ResourceName> = R extends keyof PayloadMap ? PayloadMap[R] : never;
