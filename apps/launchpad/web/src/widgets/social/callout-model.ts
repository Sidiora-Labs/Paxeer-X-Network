import type { V2ContractTypes } from '@kindle/sdk';
import type { Callout, FeedCard } from '@/core/market/use-feed';

export type PoolStats = V2ContractTypes['PoolStats'];
export type CalloutDirection = 'bullish' | 'bearish' | 'neutral';

/** One callout regardless of whether it came from the discovery feed or a pool callout list. */
export interface CalloutView {
  id: string;
  /** Author wallet when the source identifies one; null when the discovery source only carries an opaque account id. */
  authorWallet: string | null;
  /** Opaque author id shown when no wallet is available. */
  authorId: string;
  body: string;
  poolAddress: string | null;
  createdAt: string | null;
  direction: CalloutDirection | null;
  disclosure: 'organic' | 'sponsored' | 'creator' | null;
  /** Free-text position the author declared with the post; verification comes from the discovery service. */
  positionDisclosure: string | null;
  positionVerified: boolean | null;
  corrected: boolean;
  removed: boolean;
  source: 'feed' | 'pool';
}

const walletPattern = /^0x[0-9a-fA-F]{40}$/;
const directions: ReadonlySet<string> = new Set(['bullish', 'bearish', 'neutral']);
const disclosures: ReadonlySet<string> = new Set(['organic', 'sponsored', 'creator']);

const wallet = (value: string | null | undefined): string | null => (value && walletPattern.test(value) ? value.toLowerCase() : null);
const iso = (value: string | null | undefined): string | null => (value && Number.isFinite(Date.parse(value)) ? value : null);

export function feedCardView(card: FeedCard): CalloutView {
  const source = card.Source;
  const pool = source.Pools?.find(address => walletPattern.test(address)) ?? null;
  const author = wallet(source.CreatorPublicID);
  return {
    id: source.ID,
    authorWallet: author,
    authorId: source.CreatorPublicID,
    body: card.Body,
    poolAddress: pool ? pool.toLowerCase() : null,
    createdAt: iso(source.PublishedAt),
    direction: null,
    disclosure: null,
    positionDisclosure: card.PositionDisclosure.trim() ? card.PositionDisclosure : null,
    positionVerified: card.PositionVerified,
    corrected: source.Corrected,
    removed: source.Tombstone || !source.Public || card.State === 'removed' || card.State === 'tombstone',
    source: 'feed',
  };
}

export function poolCalloutView(callout: Callout): CalloutView {
  return {
    id: callout.calloutId,
    authorWallet: wallet(callout.authorWallet),
    authorId: callout.authorWallet,
    body: callout.body,
    poolAddress: wallet(callout.poolAddress),
    createdAt: iso(callout.createdAt),
    direction: directions.has(callout.direction) ? callout.direction : null,
    disclosure: disclosures.has(callout.disclosure) ? callout.disclosure : null,
    positionDisclosure: null,
    positionVerified: null,
    corrected: callout.revision !== '1',
    removed: false,
    source: 'pool',
  };
}

/** Unique pool addresses referenced by the given callouts, bounded to the stats batch limit. */
export function calloutPools(callouts: readonly CalloutView[], limit = 50): string[] {
  const pools = new Set<string>();
  for (const callout of callouts) {
    if (callout.poolAddress) pools.add(callout.poolAddress);
    if (pools.size >= limit) break;
  }
  return [...pools];
}

/**
 * Market cap used on the MC chip: the verified total-supply valuation when the
 * supply is verified, otherwise the circulating valuation; null when neither
 * is published.
 */
export function marketCapRaw(stats: PoolStats | null | undefined): string | null {
  if (!stats) return null;
  if (stats.supplyValuationQuality === 'verified' && stats.totalSupplyValuationUsdlRaw) return stats.totalSupplyValuationUsdlRaw;
  return stats.circulatingMarketCapUsdlRaw ?? null;
}

/** Callouts published within `windowMs` of `now`; undated callouts are kept only when the window is unbounded. */
export function withinWindow(callouts: readonly CalloutView[], windowMs: number | null, now = Date.now()): CalloutView[] {
  if (windowMs === null) return [...callouts];
  return callouts.filter(callout => callout.createdAt !== null && now - Date.parse(callout.createdAt) <= windowMs);
}

export const calloutWindows = { '1d': 86_400_000, '1w': 7 * 86_400_000 } as const;
export type CalloutWindow = keyof typeof calloutWindows;
