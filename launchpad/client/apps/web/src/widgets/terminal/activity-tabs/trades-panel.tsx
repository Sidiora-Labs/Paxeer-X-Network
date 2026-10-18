'use client';
import { useEffect, useMemo, useRef, useState } from 'react';
import { ArrowLeftRight, ExternalLink, Filter } from 'lucide-react';
import { EmptyState, ErrorState, LoadMore, NoticeBar, ShortAddress, Skeleton, SortHeader, Tooltip } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatTokenAmount, formatUsdl } from '@/utils/format';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { useMarketTrades } from '@/page-modules/token/market-session-context';
import type { Trade, Token } from '@/page-modules/token/market-presentation';
import type { MarketLive } from '@/widgets/coin/trades-table';
import { arrivedKeys, fillKey, fillMaker, visibleFills, type FillFilter, type FillFilterWallets, type FillSort } from './fills';
import { userFacingMessage } from '@/core/user-facing-error';

const explorerOrigin = safeExternalDestination('https://paxscan.paxeer.app/')?.replace(/\/$/, '') ?? null;
const pageSize = 100;
const noTimeNote = 'Trades are sorted by block, newest first.';
const noValuationNote = '';

export type FillsToken = Pick<Token, 'decimals' | 'decimalsQuality'>;

export interface FillsTableProps {
  rows: readonly Trade[];
  token: FillsToken;
  filter: FillFilter;
  wallets: FillFilterWallets;
  /** False while snapshot/replay continuity is unconfirmed. */
  contiguous: boolean;
}

/** Dense fills table (header 24, rows 36): Block | Side | Amount | Total USDL | Maker | Tx. */
export function FillsTable({ rows, token, filter, wallets }: FillsTableProps) {
  const [sort, setSort] = useState<FillSort>('desc');
  const [limit, setLimit] = useState(pageSize);
  const matching = useMemo(() => visibleFills(rows, filter, wallets, sort), [rows, filter, wallets, sort]);
  const shown = matching.slice(0, limit);
  const seen = useRef<Set<string> | null>(null);
  const flash = useMemo(() => arrivedKeys(seen.current, rows), [rows]);
  useEffect(() => { seen.current = new Set(rows.map(fillKey)); }, [rows]);
  useEffect(() => { setLimit(pageSize); }, [filter]);
  const identities = useWalletIdentities(shown.map(fillMaker));
  const decimals = token.decimalsQuality === 'verified' ? token.decimals : null;

  if (matching.length === 0) {
    return filter === 'all'
      ? <EmptyState icon={ArrowLeftRight} title="No trades yet" body="Trades appear here as soon as someone buys or sells this coin." className="py-6" />
      : <EmptyState icon={Filter} announce title="No trades match this filter" body="Switch back to All to see every trade on this pair." className="py-6" />;
  }
  return (
    <div className="flex flex-col">
      <p className="sr-only">{noTimeNote} {noValuationNote}</p>
      <div className="overflow-x-auto">
        <table className="w-full min-w-[640px] text-xs [&_td]:whitespace-nowrap [&_td]:px-3" aria-label="Trades">
          <thead className="sticky top-0 z-[1] bg-surface-1">
            <tr>
              <SortHeader className="h-6" label={<Tooltip content={noTimeNote}><span tabIndex={0}>Block</span></Tooltip>} sort={sort} onSort={() => setSort(sort === 'desc' ? 'asc' : 'desc')} />
              <SortHeader className="h-6" label="Type" />
              <SortHeader className="h-6" label="Amount" align="right" />
              <SortHeader className="h-6" label="USDL" align="right" />
              <SortHeader className="h-6" label="Account" />
              <SortHeader className="h-6" label="Transaction" align="right" />
            </tr>
          </thead>
          <tbody>
            {shown.map(row => {
              const key = fillKey(row);
              const maker = fillMaker(row);
              const name = identities.identities[maker]?.displayName ?? null;
              const href = explorerOrigin && /^0x[0-9a-fA-F]{64}$/.test(row.position.transactionHash) ? `${explorerOrigin}/tx/${row.position.transactionHash}` : null;
              return (
                <tr key={key} data-fill-key={key} className={cn('h-9 bg-surface-1 transition-colors hover:bg-surface-hover', flash.has(key) && (row.side === 'buy' ? 'sl-flash-up' : 'sl-flash-down'))}>
                  <td data-numeric className="font-mono tabular-nums text-text-muted">{row.position.height}</td>
                  <td className={cn('font-bold capitalize', row.side === 'buy' ? 'text-success' : 'text-danger')}>{row.side}</td>
                  <td data-numeric className="text-right tabular-nums text-text">{decimals === null ? <span className="text-text-faint" title="Token decimals are not verified">-</span> : formatTokenAmount(row.tokenAmountRaw, decimals)}</td>
                  <td data-numeric className={cn('text-right tabular-nums', row.side === 'buy' ? 'text-success' : 'text-danger')}>{formatUsdl(row.usdlAmountRaw)}</td>
                  <td>
                    <span className="inline-flex items-center gap-1.5">
                      {name ? <span className="max-w-32 truncate font-bold text-text" title={maker}>{name}</span> : null}
                      <ShortAddress address={maker} className={name ? 'text-text-faint' : 'text-text-muted'} />
                    </span>
                  </td>
                  <td className="text-right">
                    {href
                      ? <a href={href} target="_blank" rel="noopener noreferrer" title={row.position.transactionHash} className="inline-flex items-center gap-1 rounded-xs font-mono text-accent hover:underline">{row.position.transactionHash.slice(0, 8)}<ExternalLink aria-hidden="true" className="size-3" /><span className="sr-only">, opens the Paxeer explorer in a new tab</span></a>
                      : <span className="font-mono text-text-muted" title={row.position.transactionHash}>{row.position.transactionHash.slice(0, 8)}</span>}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      {matching.length > shown.length
        ? <button type="button" onClick={() => setLimit(value => value + pageSize)} className="mx-auto my-2 rounded-pill bg-surface-2 px-3 py-1 text-xs font-bold text-text-muted transition-colors hover:bg-surface-hover hover:text-text">Show {Math.min(pageSize, matching.length - shown.length)} more retained fills</button>
        : null}
    </div>
  );
}

/** Live fills for one pool: the market session's retained rows merged with IF_TRADES pages. */
export function TradesPanel({ poolAddress, token, live, filter, wallets }: { poolAddress: string; token: FillsToken; live: MarketLive; filter: FillFilter; wallets: FillFilterWallets }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const trades = useMarketTrades(poolAddress, cursor);
  useEffect(() => { setCursor(null); }, [poolAddress]);
  useEffect(() => { if (trades.data) live.session.mergePage(trades.data.data.items, trades.data.revision); }, [trades.data, live.session]);
  const watermark = live.state.watermark ?? trades.data?.watermark ?? null;
  if (watermark?.integrity === 'quarantined') return <p role="alert" className="p-4 text-sm text-danger">Trades can&apos;t load right now. Try again in a moment.</p>;
  return (
    <div className="flex flex-col gap-1">
      {trades.isError && live.state.rows.length === 0 ? <ErrorState title="Trades can't load right now" message={userFacingMessage(trades.error)} onRetry={() => { void trades.refetch(); }} retrying={trades.isFetching} /> : null}
      {trades.isError && live.state.rows.length > 0 ? <ErrorState variant="inline" message="Older trades can't load right now. Newer ones are still shown." onRetry={() => { void trades.refetch(); }} /> : null}
      {live.state.stream !== 'live' && live.state.rows.length > 0 ? <NoticeBar tone="info">Catching up...</NoticeBar> : null}
      {trades.isError && live.state.rows.length === 0
        ? null
        : trades.isPending && live.state.rows.length === 0
        ? <Skeleton variant="rows" rows={6} label="trades" />
        : <FillsTable rows={live.state.rows} token={token} filter={filter} wallets={wallets} contiguous={live.state.stream === 'live'} />}
      {trades.data
        ? <LoadMore auto={false} hasMore={trades.data.nextCursor !== null} loading={trades.isFetching} error={trades.isError} label="Load older trades" endLabel="No older trades" onLoadMore={() => { if (trades.data?.nextCursor) setCursor(trades.data.nextCursor); }} />
        : null}
    </div>
  );
}
