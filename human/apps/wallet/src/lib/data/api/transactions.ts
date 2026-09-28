/** Blockscout API v2 — Transaction endpoints (wallet-relevant subset) */

import { getBlockscoutClient } from '../client';
import type { PaginatedResponse, PaginationParams } from '../types/common';
import type { TokenTransfer, Log } from '../types/token';
import type { Transaction } from '../types/transaction';

// ── GET /v2/transactions/:hash ──────────────────────────────────────────────

export async function getTransaction(txHash: string): Promise<Transaction> {
  return getBlockscoutClient().get<Transaction>(`/v2/transactions/${txHash}`);
}

// ── GET /v2/transactions/:hash/token-transfers ──────────────────────────────

export async function getTransactionTokenTransfers(
  txHash: string,
  params?: PaginationParams,
): Promise<PaginatedResponse<TokenTransfer>> {
  return getBlockscoutClient().get<PaginatedResponse<TokenTransfer>>(
    `/v2/transactions/${txHash}/token-transfers`,
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/transactions/:hash/logs ─────────────────────────────────────────

export async function getTransactionLogs(
  txHash: string,
  params?: PaginationParams,
): Promise<PaginatedResponse<Log>> {
  return getBlockscoutClient().get<PaginatedResponse<Log>>(
    `/v2/transactions/${txHash}/logs`,
    params as Record<string, string | number | boolean | undefined>,
  );
}
