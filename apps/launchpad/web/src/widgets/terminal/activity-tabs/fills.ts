import type { Trade } from '@/page-modules/token/market-presentation';

export type FillFilter = 'all' | 'mine' | 'dev' | 'kol' | 'tracked';
export type FillSort = 'desc' | 'asc';

export interface FillFilterWallets {
  /** Connected wallet (wagmi), lowercase; null when no wallet is connected. */
  mine: string | null;
  /** Pool creator wallet from the pool snapshot, lowercase; null when unresolved. */
  dev: string | null;
}

export interface FillFilterOption {
  value: FillFilter;
  label: string;
  /** Reason the chip is disabled; null when it can be selected. */
  unavailable: string | null;
}

export const kolUnavailable = 'Coming soon';
export const trackedUnavailable = 'Coming soon';

export function fillFilterOptions(wallets: FillFilterWallets): FillFilterOption[] {
  return [
    { value: 'all', label: 'All', unavailable: null },
    { value: 'mine', label: 'Mine', unavailable: wallets.mine ? null : 'Connect wallet to see your trades' },
    { value: 'dev', label: 'Creator', unavailable: wallets.dev ? null : 'Creator not found yet' },
    { value: 'kol', label: 'KOL', unavailable: kolUnavailable },
    { value: 'tracked', label: 'Tracked', unavailable: trackedUnavailable },
  ];
}

/**
 * The wallet a fill is attributed to: the receipt- or calldata-attributed
 * recipient when the indexer proved it, otherwise the swap sender.
 */
export function fillMaker(trade: Trade): string {
  return (trade.recipientAttribution !== 'unavailable' && trade.recipient ? trade.recipient : trade.sender).toLowerCase();
}

/** A wallet participates in a fill when it is the sender or the attributed recipient. */
function involves(trade: Trade, wallet: string): boolean {
  const target = wallet.toLowerCase();
  if (trade.sender.toLowerCase() === target) return true;
  return trade.recipientAttribution !== 'unavailable' && trade.recipient !== null && trade.recipient.toLowerCase() === target;
}

export function matchesFillFilter(trade: Trade, filter: FillFilter, wallets: FillFilterWallets): boolean {
  if (filter === 'all') return true;
  if (filter === 'mine') return wallets.mine !== null && involves(trade, wallets.mine);
  if (filter === 'dev') return wallets.dev !== null && involves(trade, wallets.dev);
  return false;
}

export function fillKey(trade: Trade): string {
  return `${trade.position.chainId}:${trade.position.transactionHash.toLowerCase()}:${trade.position.logIndex}`;
}

function compareChainOrder(a: Trade, b: Trade): number {
  for (const field of ['height', 'transactionIndex', 'logIndex'] as const) {
    const x = BigInt(a.position[field]);
    const y = BigInt(b.position[field]);
    if (x !== y) return x > y ? 1 : -1;
  }
  return 0;
}

/** Filter then order by chain position (block, tx index, log index); desc = newest first. */
export function visibleFills(rows: readonly Trade[], filter: FillFilter, wallets: FillFilterWallets, sort: FillSort): Trade[] {
  const matching = rows.filter(row => matchesFillFilter(row, filter, wallets));
  return matching.sort((a, b) => (sort === 'desc' ? -compareChainOrder(a, b) : compareChainOrder(a, b)));
}

/** Keys present in `next` that were not in `previous` (the fills to flash). */
export function arrivedKeys(previous: ReadonlySet<string> | null, next: readonly Trade[]): Set<string> {
  if (previous === null) return new Set();
  const arrived = new Set<string>();
  for (const row of next) { const key = fillKey(row); if (!previous.has(key)) arrived.add(key); }
  return arrived;
}
