/** Blockscout API v2 — Token endpoints (wallet-relevant subset) */

import { getBlockscoutClient } from '../client';
import type { PaginatedResponse, PaginationParams } from '../types/common';
import type { Token, TokenBalance } from '../types/token';

// ── GET /v2/tokens ──────────────────────────────────────────────────────────

export interface TokenListParams extends PaginationParams {
  type?: string;
  sort?: string;
  order?: 'asc' | 'desc';
  q?: string;
}

export async function getTokens(
  params?: TokenListParams,
): Promise<PaginatedResponse<Token>> {
  return getBlockscoutClient().get<PaginatedResponse<Token>>(
    '/v2/tokens',
    params as Record<string, string | number | boolean | undefined>,
  );
}

// ── GET /v2/tokens/:hash ────────────────────────────────────────────────────

export async function getToken(tokenHash: string): Promise<Token> {
  return getBlockscoutClient().get<Token>(`/v2/tokens/${tokenHash}`);
}

// ── GET /v2/tokens/:hash/holders ────────────────────────────────────────────

export interface TokenHolderItem {
  address: import('../types/address').Address;
  value: string;
  token_id: string | null;
  token: Token;
}

export async function getTokenHolders(
  tokenHash: string,
  params?: PaginationParams,
): Promise<PaginatedResponse<TokenHolderItem>> {
  return getBlockscoutClient().get<PaginatedResponse<TokenHolderItem>>(
    `/v2/tokens/${tokenHash}/holders`,
    params as Record<string, string | number | boolean | undefined>,
  );
}
