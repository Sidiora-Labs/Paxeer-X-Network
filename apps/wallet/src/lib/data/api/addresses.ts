/** Blockscout API v2 — Address endpoints (wallet-relevant subset) */

import { getBlockscoutClient } from '../client';
import type { PaginatedResponse, PaginationParams } from '../types/common';
import type {
  AddressResponse,
  AddressCounters,
  AddressTabsCounters,
  CoinBalance,
  CoinBalanceHistoryByDay,
} from '../types/address';
import type {
  TokenBalance,
  TokenTransfer,
  TokenInstanceInList,
  NFTCollection,
  Log,
} from '../types/token';
import type { Transaction } from '../types/transaction';

// ── GET /v2/addresses/:hash ─────────────────────────────────────────────────

export async function getAddress(addressHash: string): Promise<AddressResponse> {
  return getBlockscoutClient().get<AddressResponse>(`/v2/addresses/${addressHash}`);
}

// ── GET /v2/addresses/:hash/counters ────────────────────────────────────────

export async function getAddressCounters(addressHash: string): Promise<AddressCounters> {
  return getBlockscoutClient().get<AddressCounters>(`/v2/addresses/${addressHash}/counters`);
}

// ── GET /v2/addresses/:hash/tabs-counters ───────────────────────────────────

export async function getAddressTabsCounters(addressHash: string): Promise<AddressTabsCounters> {
  return getBlockscoutClient().get<AddressTabsCounters>(
    `/v2/addresses/${addressHash}/tabs-counters`,
  );
}

// ── GET /v2/addresses/:hash/transactions ────────────────────────────────────

export interface AddressTxsParams extends PaginationParams {
  filter?: 'to' | 'from';
  sort?: 'block_number' | 'value' | 'fee';
  order?: 'asc' | 'desc';
}

export async function getAddressTransactions(
  addressHash: string,
  params?: AddressTxsParams,
): Promise<PaginatedResponse<Transaction>> {
  return getBlockscoutClient().get<PaginatedResponse<Transaction>>(
    `/v2/addresses/${addressHash}/transactions`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/addresses/:hash/token-transfers ─────────────────────────────────

export interface AddressTokenTransfersParams extends PaginationParams {
  filter?: 'to' | 'from';
  type?: string;
  token?: string;
}

export async function getAddressTokenTransfers(
  addressHash: string,
  params?: AddressTokenTransfersParams,
): Promise<PaginatedResponse<TokenTransfer>> {
  return getBlockscoutClient().get<PaginatedResponse<TokenTransfer>>(
    `/v2/addresses/${addressHash}/token-transfers`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/addresses/:hash/tokens (paginated token balances) ───────────────

export interface AddressTokensParams extends PaginationParams {
  type?: string;
}

export async function getAddressTokens(
  addressHash: string,
  params?: AddressTokensParams,
): Promise<PaginatedResponse<TokenBalance>> {
  return getBlockscoutClient().get<PaginatedResponse<TokenBalance>>(
    `/v2/addresses/${addressHash}/tokens`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/addresses/:hash/token-balances (all token balances, no pagination)

export async function getAddressTokenBalances(addressHash: string): Promise<TokenBalance[]> {
  return getBlockscoutClient().get<TokenBalance[]>(
    `/v2/addresses/${addressHash}/token-balances`,
  );
}

// ── GET /v2/addresses/:hash/coin-balance-history ────────────────────────────

export async function getAddressCoinBalanceHistory(
  addressHash: string,
  params?: PaginationParams,
): Promise<PaginatedResponse<CoinBalance>> {
  return getBlockscoutClient().get<PaginatedResponse<CoinBalance>>(
    `/v2/addresses/${addressHash}/coin-balance-history`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/addresses/:hash/coin-balance-history-by-day ─────────────────────

export async function getAddressCoinBalanceHistoryByDay(
  addressHash: string,
): Promise<CoinBalanceHistoryByDay> {
  return getBlockscoutClient().get<CoinBalanceHistoryByDay>(
    `/v2/addresses/${addressHash}/coin-balance-history-by-day`,
  );
}

// ── GET /v2/addresses/:hash/nft ─────────────────────────────────────────────

export interface AddressNftParams extends PaginationParams {
  type?: string;
}

export async function getAddressNfts(
  addressHash: string,
  params?: AddressNftParams,
): Promise<PaginatedResponse<TokenInstanceInList>> {
  return getBlockscoutClient().get<PaginatedResponse<TokenInstanceInList>>(
    `/v2/addresses/${addressHash}/nft`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/addresses/:hash/nft/collections ─────────────────────────────────

export async function getAddressNftCollections(
  addressHash: string,
  params?: AddressNftParams,
): Promise<PaginatedResponse<NFTCollection>> {
  return getBlockscoutClient().get<PaginatedResponse<NFTCollection>>(
    `/v2/addresses/${addressHash}/nft/collections`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/addresses/:hash/logs ────────────────────────────────────────────

export async function getAddressLogs(
  addressHash: string,
  params?: PaginationParams,
): Promise<PaginatedResponse<Log>> {
  return getBlockscoutClient().get<PaginatedResponse<Log>>(
    `/v2/addresses/${addressHash}/logs`,
    params as Record<string, string | number | boolean | undefined>,
  );
}
