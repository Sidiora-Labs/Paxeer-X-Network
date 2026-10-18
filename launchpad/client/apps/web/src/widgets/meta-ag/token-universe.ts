/** Token discovery for optional advanced views. USDL identity/scale comes
 * from the current core deployment; unqualified WPAX is never advertised. */
import { CORE_DEPLOYMENT } from '@/core/network/deployment';
import { QueryClient } from '@tanstack/react-query';
import type { Operations } from '@kindle/sdk';
import { fetchTokenMetadataBatch } from '@/core/clients/metadata';
import { publicOperations } from '@/core/market/operations';
import { fetchTokenStatsBatch } from '@/hooks/market/use-token-stats';
import { rankingList } from '@/hooks/market/use-ranking';
import { appendRankingPage, marketLimits } from '@/page-modules/token/market-session';
import {
  META_AG_USDL_ADDRESS,
} from '@/core/clients/meta-ag';
import type { TokenMetadata } from '@/widgets/home/types';

export interface MetaAgToken {
  /** Lowercased ERC20 address — used as canonical key. */
  tokenAddress: `0x${string}`;
  /** Pool address on the launchpad, if any (null for USDL/WPAX). */
  poolAddress: `0x${string}` | null;
  name: string;
  symbol: string;
  logo: string | null;
  decimals: number;
  /** USD price per token. 0 if unknown (v2 PoolStats carries no price). */
  price: number;
  /** Market cap in USD (number): PoolStats.totalSupplyValuationUsdlRaw. 0 if unknown. */
  marketCap: number;
  /**
   * `quote` = canonical quote token (USDL, WPAX). Always shown first
   * in the selector and used as the default in/out pair.
   * `launchpad` = token discovered through the AMM ranking feed.
   */
  kind: 'quote' | 'launchpad';
}

const QUOTE_TOKENS: MetaAgToken[] = [
  {
    tokenAddress: META_AG_USDL_ADDRESS.toLowerCase() as `0x${string}`,
    poolAddress: null,
    name: 'USD Liquid',
    symbol: 'USDL',
    logo: '/usdl-logo.png',
    decimals: CORE_DEPLOYMENT.usdlDecimals,
    price: 0,
    marketCap: 0,
    kind: 'quote',
  },

];

/** Session cache for pool -> token listings when no React QueryClient is supplied. */
let sessionCache: QueryClient | null = null;

export interface MetaAgTokenSources {
  operations?: Operations;
  queryClient?: QueryClient;
}

/**
 * Quote tokens first, then the launchpad tokens of the trending ranking:
 * IF_RANKINGS `GET /v2/rankings/trending` (first page, rank order via
 * rankingList), IF_STATS_BATCH for those pools joined with their tokens
 * (fetchTokenStatsBatch) and the metadata batch for names and logos.
 * Any failure leaves the quote tokens only.
 */
export async function loadMetaAgTokens(
  signal?: AbortSignal,
  sources: MetaAgTokenSources = {},
): Promise<MetaAgToken[]> {
  // Always include the canonical quote tokens up front so the selector
  // works even when the ranking projection is unavailable.
  const tokens: MetaAgToken[] = [...QUOTE_TOKENS];
  const operations = sources.operations ?? publicOperations();
  const queryClient = sources.queryClient ?? (sessionCache ??= new QueryClient());

  try {
    const response = await operations.call('IF_RANKINGS', { path: { category: 'trending' } }, { signal });
    if (response.data.snapshot.category !== 'trending') throw new Error('Ranking category disagreement');
    const ranking = rankingList(appendRankingPage(null, response).data.snapshot, marketLimits.rankingPage, 0);
    const poolAddrs = ranking.items.map(item => item.poolAddress);
    if (poolAddrs.length === 0) return tokens;

    const statsMap = await fetchTokenStatsBatch(queryClient, operations, operations.client.config.apiOrigin, poolAddrs, signal);
    const tokenAddrs = poolAddrs.map(pool => statsMap[pool]?.tokenAddress ?? null).filter((value): value is string => value !== null);
    const metaByToken = await fetchTokenMetadataBatch(tokenAddrs, signal);

    for (const poolAddr of poolAddrs) {
      const stats = statsMap[poolAddr];
      if (!stats) continue;
      // De-dupe against the quote tokens (the curated entry wins).
      if (tokens.some(t => t.tokenAddress === stats.tokenAddress)) continue;
      const meta: TokenMetadata | null = metaByToken[stats.tokenAddress] ?? null;
      tokens.push({
        tokenAddress: stats.tokenAddress as `0x${string}`,
        poolAddress: poolAddr as `0x${string}`,
        name: meta?.name || `Token ${poolAddr.slice(0, 6)}`,
        symbol: meta?.symbol || '???',
        logo: meta?.images?.logo || null,
        // Launchpad tokens are 6-dec by factory contract.
        decimals: 6,
        // PoolStats carries no per-token price; 0 means unknown.
        price: 0,
        marketCap: stats.marketCap === null ? 0 : Number(stats.marketCap),
        kind: 'launchpad',
      });
    }
  } catch (err) {
    if ((err as DOMException)?.name !== 'AbortError') {
      console.warn('meta-ag: token universe load partial failure:', err);
    }
  }

  return tokens;
}
