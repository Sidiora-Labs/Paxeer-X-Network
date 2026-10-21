'use client';
import TabUnavailable from './tab-unavailable';
import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { CircleHelp, SearchX, Users } from 'lucide-react';
import { Avatar, CountPill, EmptyState, ErrorState, LoadMore, NoticeBar, Skeleton, SortHeader, Tooltip, UnavailableState, type SortDirection } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useHolders, type HoldersPage } from '@/core/market/use-holders';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { formatShare, statsShare, statsSpotPrice } from '@/core/market/pool-stats-display';
import { StatDash } from '@/ui/shared/stat-value';
import { formatPercent, formatTokenAmount, formatUsdl, rawToDecimalString, shortAddress } from '@/utils/format';
import HoldersFilters, { type HolderSearch } from './holders-filters';
import { userFacingMessage } from '@/core/user-facing-error';

export interface HolderRow {
  wallet: string;
  balanceRaw: string;
  /** Percent of total supply (0-100); null when no source can state it. */
  supplyPercent: number | null;
  /** Position in raw USDL (6 decimals); null without a verified balance scale and a price. */
  positionUsdlRaw: bigint | null;
}

type SortKey = 'held' | 'supply';
const defaultSort: { key: SortKey; direction: SortDirection } = { key: 'held', direction: 'desc' };
const pnlOperation = 'Coming soon';
const pnlReason = 'Profit, average entry and amount bought for each holder are coming soon.';

function ratioPercent(numerator: string, denominator: string): number | null {
  if (!/^(0|[1-9][0-9]*)$/.test(numerator) || !/^[1-9][0-9]*$/.test(denominator)) return null;
  const scaled = (BigInt(numerator) * 1000000n) / BigInt(denominator);
  return Number(scaled) / 10000;
}

export function buildHolderRows(pages: readonly HoldersPage[], token: { decimals: number | null; decimalsQuality: 'verified' | 'unavailable'; totalSupplyRaw: string | null; supplyQuality: 'verified' | 'partial' | 'unavailable' } | null, price: number | null): HolderRow[] {
  const decimals = token?.decimalsQuality === 'verified' && token.decimals !== null ? token.decimals : null;
  const supply = token?.supplyQuality === 'verified' && token.totalSupplyRaw && /^[1-9][0-9]*$/.test(token.totalSupplyRaw) ? token.totalSupplyRaw : null;
  const byWallet = new Map<string, HolderRow>();
  const legacy = new Map<string, { balance: string; pctOfSupply: string }>();
  for (const page of pages) for (const [wallet, share] of Object.entries(page.legacyShare ?? {})) legacy.set(wallet.toLowerCase(), share);
  const position = (balanceRaw: string): bigint | null => {
    if (decimals === null || price === null || !Number.isFinite(price)) return null;
    const human = Number(rawToDecimalString(balanceRaw, decimals));
    if (!Number.isFinite(human)) return null;
    const usdl = human * price;
    return Number.isFinite(usdl) && usdl < 9e15 ? BigInt(Math.round(usdl * 1000000)) : null;
  };
  const supplyShare = (wallet: string, balanceRaw: string, share: { numerator: string; denominator: string } | null): number | null => {
    if (share) { const value = ratioPercent(share.numerator, share.denominator); if (value !== null) return value; }
    const legacyRow = legacy.get(wallet);
    if (legacyRow && /^-?\d+$/.test(legacyRow.pctOfSupply)) return Number(legacyRow.pctOfSupply) / 100;
    if (supply && /^(0|[1-9][0-9]*)$/.test(balanceRaw)) return ratioPercent(balanceRaw, supply);
    return null;
  };
  for (const page of pages) for (const entry of page.rows) {
    const wallet = entry.wallet.toLowerCase();
    byWallet.set(wallet, { wallet, balanceRaw: entry.balanceRaw, supplyPercent: supplyShare(wallet, entry.balanceRaw, entry.share), positionUsdlRaw: position(entry.balanceRaw) });
  }
  if (byWallet.size === 0) for (const [wallet, share] of legacy) if (/^(0|[1-9][0-9]*)$/.test(share.balance)) byWallet.set(wallet, { wallet, balanceRaw: share.balance, supplyPercent: supplyShare(wallet, share.balance, null), positionUsdlRaw: position(share.balance) });
  return [...byWallet.values()];
}

export function sortHolderRows(rows: readonly HolderRow[], key: SortKey, direction: SortDirection): HolderRow[] {
  const sign = direction === 'asc' ? 1 : -1;
  return [...rows].sort((a, b) => {
    if (key === 'held') { const x = BigInt(a.balanceRaw), y = BigInt(b.balanceRaw); return x === y ? a.wallet.localeCompare(b.wallet) : (x > y ? 1 : -1) * sign; }
    const x = a.supplyPercent ?? -1, y = b.supplyPercent ?? -1;
    return x === y ? a.wallet.localeCompare(b.wallet) : (x - y) * sign;
  });
}

export function matchesHolderSearch(row: HolderRow, displayName: string | null, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  const hex = needle.startsWith('0x') ? needle : `0x${needle}`;
  if (row.wallet.startsWith(hex)) return true;
  return displayName !== null && displayName.toLowerCase().includes(needle);
}

/** Holder concentration chip over the table (PoolStats.top10HolderShareBps / creatorHoldingShareBps), or its dash with the reason. */
function ShareChip({ label, spoken, share }: { label: string; spoken: string; share: { value: number | null; reason: string | null } }) {
  return (
    <span data-share-chip={label} className="inline-flex items-center gap-1 rounded-pill bg-surface-2 px-2 py-0.5 text-xs text-text-faint">
      <span aria-hidden="true">{label}</span>
      {share.value === null
        ? <StatDash reason={share.reason ?? 'Not available yet'} subject={spoken} align="start" />
        : <span data-numeric className="font-bold tabular-nums text-text-muted"><span className="sr-only">{spoken} </span>{formatShare(share.value)}</span>}
    </span>
  );
}

function HoldersBody({ pool, price }: { pool: string; price: number | null }) {
  const snapshot = usePoolSnapshot(pool);
  const [cursor, setCursor] = useState<string | null>(null);
  const [pages, setPages] = useState<Record<string, HoldersPage>>({});
  const [sort, setSort] = useState(defaultSort);
  const [search, setSearch] = useState<HolderSearch>({ query: '', filter: 'all' });
  const holders = useHolders(pool, cursor);
  useEffect(() => { setCursor(null); setPages({}); setSort(defaultSort); setSearch({ query: '', filter: 'all' }); }, [pool]);
  const pageKey = cursor ?? 'first';
  const readyPage = holders.state === 'ready' ? holders.data : null;
  useEffect(() => { if (readyPage) setPages(current => current[pageKey] ? current : { ...current, [pageKey]: readyPage }); }, [readyPage, pageKey]);
  const loaded = useMemo(() => Object.values(pages), [pages]);
  const token = snapshot.data?.token ?? null;
  // The live quote first; else PoolStats.spotPriceWad (null before the first trade: positions stay unavailable).
  const spot = statsSpotPrice(snapshot.data?.stats ?? null).value;
  const livePrice = price ?? (spot !== null && Number.isFinite(Number(spot)) ? Number(spot) : null);
  const stats = snapshot.data?.stats ?? null;
  const top10 = stats ? statsShare(stats, 'top10') : null;
  const devShare = stats ? statsShare(stats, 'creator') : null;
  const creator = snapshot.data?.creator?.toLowerCase() ?? null;
  const rows = useMemo(() => buildHolderRows(loaded, token, livePrice), [loaded, token, livePrice]);
  const identities = useWalletIdentities(rows.map(row => row.wallet));
  const visible = useMemo(() => sortHolderRows(rows.filter(row => matchesHolderSearch(row, identities.identities[row.wallet]?.displayName ?? null, search.query)), sort.key, sort.direction), [rows, identities.identities, search.query, sort]);
  const last = loaded[loaded.length - 1] ?? null;
  const hasMore = last?.nextCursor !== null && last?.nextCursor !== undefined && loaded.length > 0;
  const decimals = token?.decimalsQuality === 'verified' ? token.decimals : null;
  const coverage = last?.response?.data.coverage ?? null;
  const total = snapshot.data?.stats?.holderCount !== null && snapshot.data?.stats?.holderCount !== undefined ? Number(snapshot.data.stats.holderCount) : null;
  const count = total !== null && Number.isSafeInteger(total) ? total : coverage === 'complete' && !hasMore ? rows.length : null;

  function cycle(key: SortKey) {
    setSort(current => {
      if (current.key !== key) return { key, direction: 'desc' };
      if (current.direction === 'desc') return { key, direction: 'asc' };
      return defaultSort;
    });
  }

  if (holders.state === 'loading' && loaded.length === 0) return <Skeleton variant="rows" rows={6} label="holders" />;
  if (holders.state === 'unsupported' && loaded.length === 0) return <UnavailableState title="Holders" plural operation={holders.operation} status={holders.status} message="Holders can't load right now. Check back soon." />;
  if (holders.state === 'error' && loaded.length === 0) return <ErrorState title="Holders can't load right now" message={userFacingMessage(holders.error)} onRetry={holders.retry} />;

  return <div className="flex flex-col gap-3">
    <div className="flex flex-wrap items-center gap-2">
      <h3 className="flex items-center gap-2 text-sm font-bold text-text">Holders {count !== null ? <CountPill count={count} label="holders" /> : null}</h3>
      {top10 ? <ShareChip label="Top 10" spoken="Top 10 holders hold" share={top10} /> : null}
      {devShare ? <ShareChip label="Creator" spoken="Creator holds" share={devShare} /> : null}
      {last?.canonical ? <UnavailableState variant="inline" title="The full holder list" operation={last.canonical.operation} status={last.canonical.status} className="basis-full" /> : null}
    </div>
    <HoldersFilters value={search} onChange={setSearch} pnlReason={pnlReason} />
    {coverage && coverage !== 'complete' ? <NoticeBar tone="warning" announce>Some holders may be missing from this list.</NoticeBar> : null}
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] text-sm">
        <thead>
          <tr>
            <SortHeader label="Holder" />
            <SortHeader label="Held" align="right" sort={sort.key === 'held' ? sort.direction : null} onSort={() => cycle('held')} />
            <SortHeader label="% supply" align="right" sort={sort.key === 'supply' ? sort.direction : null} onSort={() => cycle('supply')} />
            <SortHeader label="Position" align="right" />
            <th scope="colgroup" colSpan={3} className="h-table-header-height px-3 text-right text-2xs font-bold uppercase tracking-[.04em] text-text-faint">
              <span className="inline-flex items-center justify-end gap-1">
                <span>Profit / Avg entry / Bought</span>
                <Tooltip content={pnlReason}>
                  <button type="button" aria-label="About profit, average entry and bought" className="inline-flex size-5 items-center justify-center rounded-xs text-text-faint transition-colors hover:text-text"><CircleHelp aria-hidden="true" className="size-3.5" /></button>
                </Tooltip>
              </span>
            </th>
          </tr>
        </thead>
        <tbody>
          {visible.length === 0 ? <EmptyState variant="table" colSpan={7} icon={search.query ? SearchX : Users} title={search.query ? 'No holders match' : 'No holders yet'} body={search.query ? `Nothing matched "${search.query}".` : 'Holders appear here after the first buy.'} announce={Boolean(search.query)} /> : null}
          {visible.map(row => {
            const identity = identities.identities[row.wallet];
            const name = identity?.displayName ?? shortAddress(row.wallet);
            return <tr key={row.wallet} className="h-table-row bg-surface-1 transition-colors hover:bg-surface-hover [&>td]:px-3">
              <td className="min-w-0">
                <Link href={`/profile/${row.wallet}`} className="inline-flex min-w-0 items-center gap-2 rounded-xs">
                  <Avatar src={identity?.avatarUrl} name={name} size="sm" decorative loading={identity?.resolution === 'pending'} />
                  <span className="flex min-w-0 flex-col leading-tight">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span className={identity?.displayName ? 'truncate font-bold text-text' : 'font-mono text-xs text-text-muted'}>{name}</span>
                      {row.wallet === creator ? <span data-dev-badge title="Creator wallet" className="shrink-0 rounded-pill bg-surface-3 px-1.5 text-2xs font-bold text-text-muted">Creator</span> : null}
                    </span>
                    {identity?.displayName ? <span className="font-mono text-2xs text-text-faint">{shortAddress(row.wallet)}</span> : null}
                  </span>
                </Link>
              </td>
              <td data-numeric className="text-right font-bold tabular-nums text-text">{decimals === null ? <span className="text-text-faint">—</span> : formatTokenAmount(row.balanceRaw, decimals)}</td>
              <td data-numeric className="text-right tabular-nums text-text-muted">{row.supplyPercent === null ? <span className="text-text-faint">—</span> : formatPercent(row.supplyPercent, { sign: 'negative' })}</td>
              <td data-numeric className="text-right tabular-nums text-text">{row.positionUsdlRaw === null ? <span className="text-text-faint">—</span> : formatUsdl(row.positionUsdlRaw, { compact: true })}</td>
              <td colSpan={3} className="text-right text-xs text-text-faint" title={pnlOperation}>—</td>
            </tr>;
          })}
        </tbody>
      </table>
    </div>
    {loaded.length > 0 ? <LoadMore auto={false} hasMore={hasMore} loading={holders.state === 'loading'} error={holders.state === 'error'} label="Load more holders" endLabel="That's everyone" onLoadMore={() => { if (last?.nextCursor) setCursor(last.nextCursor); }} /> : null}
  </div>;
}

export default function HoldersTable({ poolAddress, price = null }: { poolAddress: string; price?: number | null }) {
  const snapshot = usePoolSnapshot(poolAddress);
  if (snapshot.isPending) return <Skeleton variant="rows" rows={6} label="holders" />;
  if (snapshot.isError) return <TabUnavailable title="Holders" error={snapshot.error} onRetry={() => { void snapshot.refetch(); }} />;
  return <HoldersBody pool={snapshot.data.pool.poolAddress} price={price} />;
}
