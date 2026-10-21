/** Blockscout API v2 — Search endpoint */

import { getBlockscoutClient } from '../client';

export interface SearchResultItem {
  type: 'token' | 'address' | 'block' | 'transaction' | 'contract';
  name: string | null;
  address: string | null;
  symbol: string | null;
  token_type: string | null;
  decimals: number | null;
  icon_url: string | null;
  exchange_rate: string | null;
  url: string | null;
  block_number: number | null;
  block_hash: string | null;
  tx_hash: string | null;
  holder_count: number | null;
  is_smart_contract_verified: boolean | null;
}

export interface SearchResponse {
  items: SearchResultItem[];
  next_page_params: Record<string, unknown> | null;
}

export async function search(query: string, params?: { q?: string }): Promise<SearchResponse> {
  return getBlockscoutClient().get<SearchResponse>('/v2/search', {
    q: query,
    ...params,
  });
}
