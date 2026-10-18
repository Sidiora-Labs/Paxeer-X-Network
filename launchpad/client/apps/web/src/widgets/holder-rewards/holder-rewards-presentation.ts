import type { TokenMetadata } from '@/widgets/home/types';
import { safeExternalDestination, poolPath } from '@/core/app-foundation/navigation';
import { formatCount, formatUsdl, shortAddress } from '@/utils/format';
import type { HolderRewardPool, HolderRewardsSort, PoolStats } from './holder-rewards-contract';
import { marketCapRaw } from '@/core/market/pool-stats-display';

export type HolderRewardsSortKey = HolderRewardsSort;
export type HolderRewardsDirection = 'asc' | 'desc';
export interface HolderRewardsOrder { key: HolderRewardsSortKey; direction: HolderRewardsDirection }

export const defaultOrder: HolderRewardsOrder = Object.freeze({ key: 'rewards', direction: 'desc' });

export const sortOptions: ReadonlyArray<{ value: HolderRewardsSortKey; label: string }> = Object.freeze([
  { value: 'rewards', label: 'Holder rewards' },
  { value: 'mcap', label: 'Market cap' },
  { value: 'holders', label: 'Holders paid' },
  { value: 'age', label: 'Newest' },
]);

export type SocialKind = 'website' | 'twitter' | 'telegram';
export interface SocialLink { kind: SocialKind; href: string }

export interface HolderRewardRow {
  pool: HolderRewardPool;
  poolAddress: string;
  tokenAddress: string;
  name: string;
  symbol: string;
  image: string | null;
  links: SocialLink[];
  /** Circulating market cap in raw USDL (6 decimals); null until IF_STATS_BATCH answers for the pool. */
  mcapRaw: string | null;
  stats: PoolStats | null;
}

const socialKinds: ReadonlyArray<SocialKind> = ['website', 'twitter', 'telegram'];

/** Only destinations the shell's external-link guard would let through (core/app-foundation/navigation.ts safeExternalDestination). */
export function approvedSocialLinks(metadata: TokenMetadata | null | undefined): SocialLink[] {
  const socials = metadata?.socials;
  if (!socials) return [];
  const links: SocialLink[] = [];
  for (const kind of socialKinds) {
    const raw = socials[kind];
    if (typeof raw !== 'string') continue;
    const href = safeExternalDestination(raw);
    if (href) links.push({ kind, href });
  }
  return links;
}

export function explorerHref(address: string): string {
  return `https://paxscan.paxeer.app/address/${address.toLowerCase()}`;
}

export function coinHref(poolAddress: string): string {
  return poolPath(125, poolAddress);
}

export function buildRows(items: readonly HolderRewardPool[], stats: ReadonlyMap<string, PoolStats>, metadata: Record<string, TokenMetadata | null> | undefined): HolderRewardRow[] {
  return items.map(item => {
    const poolAddress = item.poolAddress.toLowerCase();
    const tokenAddress = item.tokenAddress.toLowerCase();
    const meta = metadata?.[tokenAddress] ?? null;
    const poolStats = stats.get(poolAddress) ?? null;
    const symbol = meta?.symbol?.trim() || '';
    const name = meta?.name?.trim() || symbol || shortAddress(tokenAddress);
    return {
      pool: item,
      poolAddress,
      tokenAddress,
      name,
      symbol,
      image: meta?.images?.logo ?? null,
      links: approvedSocialLinks(meta),
      mcapRaw: marketCapRaw(poolStats),
      stats: poolStats,
    };
  });
}

function compareBig(a: string | null, b: string | null): number {
  if (a === null && b === null) return 0;
  if (a === null) return 1;
  if (b === null) return -1;
  const x = BigInt(a);
  const y = BigInt(b);
  return x === y ? 0 : x > y ? -1 : 1;
}

/** Descending comparator for the key; unknown market caps always sort last regardless of direction. */
function compareDescending(key: HolderRewardsSortKey, a: HolderRewardRow, b: HolderRewardRow): number {
  switch (key) {
    case 'rewards': return compareBig(a.pool.distributedRaw, b.pool.distributedRaw);
    case 'mcap': return compareBig(a.mcapRaw, b.mcapRaw);
    case 'holders': return b.pool.holdersPaid - a.pool.holdersPaid;
    case 'age': return Date.parse(b.pool.createdAt) - Date.parse(a.pool.createdAt);
  }
}

export function compareRows(order: HolderRewardsOrder): (a: HolderRewardRow, b: HolderRewardRow) => number {
  return (a, b) => {
    if (order.key === 'mcap' && (a.mcapRaw === null) !== (b.mcapRaw === null)) return a.mcapRaw === null ? 1 : -1;
    const base = compareDescending(order.key, a, b);
    const directed = order.direction === 'desc' ? base : -base;
    return directed !== 0 ? directed : a.poolAddress.localeCompare(b.poolAddress);
  };
}

export function sortRows(rows: readonly HolderRewardRow[], order: HolderRewardsOrder): HolderRewardRow[] {
  return [...rows].sort(compareRows(order));
}

/** Column header cycle: another column -> desc, desc -> asc, asc -> the default order. */
export function nextOrder(current: HolderRewardsOrder, key: HolderRewardsSortKey): HolderRewardsOrder {
  if (current.key !== key) return { key, direction: 'desc' };
  if (current.direction === 'desc') return { key, direction: 'asc' };
  return defaultOrder;
}

export function sortLabel(key: HolderRewardsSortKey): string {
  return sortOptions.find(option => option.value === key)?.label ?? key;
}

const oneUsdlRaw = 1_000_000n;

/** Whole-unit USDL for the rewards cell; sub-unit totals read "< 1 USDL". */
export function rewardsText(raw: string | null | undefined): string {
  if (typeof raw === 'string' && /^\d+$/.test(raw)) {
    const value = BigInt(raw);
    if (value > 0n && value < oneUsdlRaw) return '< 1 USDL';
  }
  return formatUsdl(raw, { fractionDigits: 0 });
}

export function mcapText(raw: string | null | undefined): string {
  return formatUsdl(raw, { compact: true, unit: false });
}

/** Full grouped count ("14,726"), as the reference prints it; compact notation hid up to 99 wallets. */
export function holdersPaidText(count: number | null | undefined): string {
  if (typeof count !== 'number' || !Number.isSafeInteger(count) || count < 0) return formatCount(count);
  return count.toLocaleString('en-US');
}

export function airdropText(raw: bigint): string {
  return formatUsdl(raw);
}

/** Client-side filters over the loaded rows; nothing here is inferred, rows only drop out. */
export interface HolderRewardsFilters {
  /** Keep only pools whose fee strategy is still Holders. */
  activeOnly: boolean;
  /** Keep only pools IF_STATS_BATCH priced (a known market cap). */
  pricedOnly: boolean;
}

export const defaultFilters: HolderRewardsFilters = Object.freeze({ activeOnly: false, pricedOnly: false });

export function activeFilterCount(filters: HolderRewardsFilters): number {
  return Number(filters.activeOnly) + Number(filters.pricedOnly);
}

export function filterRows(rows: readonly HolderRewardRow[], filters: HolderRewardsFilters): HolderRewardRow[] {
  return rows.filter(row => (!filters.activeOnly || row.pool.strategyActive) && (!filters.pricedOnly || row.mcapRaw !== null));
}

function csvCell(value: string): string {
  // Leading formula characters are neutralised so a spreadsheet never evaluates a token name.
  const safe = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  return /[",\n\r]/.test(safe) ? `"${safe.replace(/"/g, '""')}"` : safe;
}

function rawUsdlDecimal(raw: string | null): string {
  if (raw === null || !/^\d+$/.test(raw)) return '';
  const padded = raw.padStart(7, '0');
  return `${padded.slice(0, -6).replace(/^0+(?=\d)/, '')}.${padded.slice(-6)}`;
}

export const csvColumns = Object.freeze(['rank', 'name', 'symbol', 'token_address', 'pool_address', 'holder_rewards_usdl', 'market_cap_usdl', 'holders_paid', 'strategy_active', 'created_at']);

/** CSV of exactly the rows shown, in the shown order; amounts are exact 6-decimal USDL strings from the raw values. */
export function rowsToCsv(rows: readonly HolderRewardRow[]): string {
  const lines = [csvColumns.join(',')];
  rows.forEach((row, index) => {
    lines.push([
      String(index + 1),
      row.name,
      row.symbol,
      row.tokenAddress,
      row.poolAddress,
      rawUsdlDecimal(row.pool.distributedRaw),
      rawUsdlDecimal(row.mcapRaw),
      String(row.pool.holdersPaid),
      row.pool.strategyActive ? 'true' : 'false',
      row.pool.createdAt,
    ].map(csvCell).join(','));
  });
  return `${lines.join('\n')}\n`;
}

/** Display numeral for the stat bar and cards: the amount without the unit, which is rendered smaller beside it. */
export function usdlAmountText(raw: string | null | undefined): string {
  const text = rewardsText(raw);
  return text.endsWith(' USDL') ? text.slice(0, -5) : text;
}
