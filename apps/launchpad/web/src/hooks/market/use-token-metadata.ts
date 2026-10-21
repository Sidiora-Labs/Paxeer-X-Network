'use client';

import { useMemo } from 'react';
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query';
import { queryKeys } from '@/core/query-keys';
import {
  fetchMetadataMedia,
  fetchMetadataV2,
  fetchTokenMetadata,
  fetchTokenMetadataBatch,
} from '@/core/clients/metadata';
import type { TokenMetadata } from '@/widgets/home/types';

const METADATA_STALE_MS = 5 * 60_000; // metadata rarely changes

/**
 * Fetch metadata for a single token. Cached and deduplicated across
 * components via React Query. If a parent has already loaded this
 * address through `useTokenMetadataBatch`, this hook returns the
 * cached value instantly with no network call.
 */
export function useTokenMetadata(
  tokenAddress: string | undefined | null,
  opts?: { enabled?: boolean },
) {
  return useQuery<TokenMetadata | null>({
    queryKey: queryKeys.tokenMetadata(tokenAddress ?? ''),
    queryFn: ({signal}) => fetchTokenMetadata(tokenAddress as string, signal),
    enabled: (opts?.enabled ?? true) && !!tokenAddress,
    staleTime: METADATA_STALE_MS,
  });
}

/**
 * Seed the per-address React Query cache from a batch response so that
 * child components reading the same addresses through `useTokenMetadata`
 * find the data already in cache and don't issue extra network calls.
 */
function seedPerAddressCache(
  qc: QueryClient,
  data: Record<string, TokenMetadata | null>,
) {
  for (const [addr, meta] of Object.entries(data)) {
    qc.setQueryData(queryKeys.tokenMetadata(addr), meta);
  }
}

/**
 * Fetch metadata for many token addresses in ONE network call via
 * `GET /metadata/batch`. Falls back to per-token fetches automatically
 * if the backend doesn't support the batch endpoint.
 *
 * Returns React Query state with `data` typed as a map of
 * `{ [lowercasedAddress]: TokenMetadata | null }` for direct lookup.
 *
 * Side effect: when the batch resolves, every address is also written
 * into the per-address React Query cache (`queryKeys.tokenMetadata`)
 * so deeply-nested children using `useTokenMetadata(addr)` reuse it.
 */
export function useTokenMetadataBatch(
  tokenAddresses: string[],
  opts?: { enabled?: boolean },
) {
  const qc = useQueryClient();

  // Stable, sorted, deduped key. Must be wrapped in useMemo so the
  // queryKey identity matches across renders for the same input set.
  const unique = useMemo(
    () => [...new Set(tokenAddresses.map((a) => a.toLowerCase()))],
    [tokenAddresses],
  );

  return useQuery<Record<string, TokenMetadata | null>>({
    queryKey: queryKeys.tokenMetadataBatch(unique),
    queryFn: async ({ signal }) => {
      const data = await fetchTokenMetadataBatch(unique, signal);
      seedPerAddressCache(qc, data);
      return data;
    },
    enabled: (opts?.enabled ?? true) && unique.length > 0,
    staleTime: METADATA_STALE_MS,
  });
}

/**
 * Standalone batch fetch for use outside React hooks (e.g. inside the
 * `queryFn` of a higher-level query). Routes through the batch client
 * with automatic per-token fallback. Returns a map keyed by lowercased
 * address (missing entries are `null`).
 */
export async function getTokenMetadataBatch(
  tokens: string[],
  signal?: AbortSignal,
): Promise<Record<string, TokenMetadata | null>> {
  return fetchTokenMetadataBatch(tokens, signal);
}

export function useMetadataV2(tokenAddress:string|undefined|null){
 const token=tokenAddress?.toLowerCase()??'';
 return useQuery({queryKey:['metadata-v2',125,token],queryFn:({signal})=>fetchMetadataV2(token,signal),enabled:/^0x[a-f0-9]{40}$/.test(token),staleTime:60000,retry:false});
}

export function useMetadataMedia(tokenAddress:string|undefined|null){
 const token=tokenAddress?.toLowerCase()??'';
 return useQuery({queryKey:['metadata-media-v2',125,token],queryFn:({signal})=>fetchMetadataMedia(token,signal),enabled:/^0x[a-f0-9]{40}$/.test(token),staleTime:60000,retry:false});
}
