import type { TokenMetadata } from '@/widgets/home/types';
import type { ExploreRow } from '@/widgets/explore/use-explore-rows';
import { safeSocialUrl } from '@/widgets/trading/token-description';
import { formatPriceFromDecimalString } from '@/utils/units';
import type { Stage } from '@/ui/primitives';
import { terminalPoolPath } from '../sub-nav';
import { windowChange, type RailTimeframe, type WindowChange } from './timeframe';

export type SocialKind = 'website' | 'twitter' | 'telegram' | 'discord';
export interface RailSocial { kind: SocialKind; url: string }

export interface RailPairRow {
  poolAddress: string;
  href: string;
  name: string;
  symbol: string;
  image: string | null;
  stage: Stage | null;
  createdAtMs: number | null;
  holders: number | null;
  mcap: number | null;
  change: WindowChange;
  socials: RailSocial[];
}

export interface RecentRow {
  poolAddress: string;
  href: string;
  name: string;
  symbol: string;
  image: string | null;
  /** Decimal USDL per whole token; null renders a dash with `priceReason`. */
  price: string | null;
  priceReason: string | null;
  mcap: number | null;
}

const socialKinds: SocialKind[] = ['website', 'twitter', 'telegram', 'discord'];

/** Approved-origin social links from the token metadata; anything off the allowlist is dropped. */
export function railSocials(metadata: TokenMetadata | null | undefined, approvedOrigins?: readonly string[]): RailSocial[] {
  const socials = metadata?.socials;
  if (!socials) return [];
  const out: RailSocial[] = [];
  for (const kind of socialKinds) {
    const raw = socials[kind];
    if (!raw) continue;
    const url = approvedOrigins ? safeSocialUrl(raw, approvedOrigins) : safeSocialUrl(raw);
    if (url) out.push({ kind, url });
  }
  return out;
}

export function toRailPairRow(row: ExploreRow, metadata: TokenMetadata | null, timeframe: RailTimeframe, approvedOrigins?: readonly string[]): RailPairRow {
  return {
    poolAddress: row.poolAddress,
    href: terminalPoolPath(row.poolAddress),
    name: row.name,
    symbol: row.symbol,
    image: row.image,
    stage: row.stage,
    createdAtMs: row.createdAtMs,
    holders: row.holders,
    mcap: row.mcap,
    change: windowChange(row, timeframe),
    socials: railSocials(metadata, approvedOrigins),
  };
}

/**
 * `price` is the decimal USDL-per-token string to show: PoolStats.spotPriceWad
 * when the stats row carries it (null there means no trade yet), else the
 * IF_POOL_DETAIL reserve price for a row from an older producer. Anything
 * that is not a plain decimal renders as unavailable.
 */
export function toRecentRow(row: ExploreRow, price: string | null, priceReason: string | null = null): RecentRow {
  const plain = price !== null && /^\d+(\.\d+)?$/.test(price.trim()) ? price.trim() : null;
  return {
    poolAddress: row.poolAddress,
    href: terminalPoolPath(row.poolAddress),
    name: row.name,
    symbol: row.symbol,
    image: row.image,
    price: plain,
    priceReason: plain === null ? priceReason ?? 'The price is not available for this coin yet.' : null,
    mcap: row.mcap,
  };
}

/** Decimal price in USDL with subscript-zero notation for sub-cent values; em dash when not served. */
export function formatRailPrice(price: string | null): string {
  return price === null ? '—' : formatPriceFromDecimalString(price);
}
