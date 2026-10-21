'use client';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { ArrowLeftRight, ExternalLink, Filter } from 'lucide-react';
import { Chip, EmptyState, SortHeader } from '@/ui/primitives';
import { formatTokenAmount, formatUsdl, shortAddress } from '@/utils/format';
import { exactRatio, orderedTrades, type Pool, type Token, type Trade, type Watermark } from './market-presentation';

export type TradeFeedPool = Pick<Pool, 'chainId' | 'poolAddress' | 'tokenAddress'>;
export type TradeFeedToken = Pick<Token, 'address' | 'decimals' | 'decimalsQuality'>;
type SideFilter = 'all' | 'buy' | 'sell';
const sideFilters: ReadonlyArray<{ value: SideFilter; label: string }> = [{ value: 'all', label: 'All' }, { value: 'buy', label: 'Buys' }, { value: 'sell', label: 'Sells' }];
const rowHeight = 48;

function explorerOriginFrom(explorerOrigin: string | undefined): string | null {
  if (!explorerOrigin) return null;
  try {
    const parsed = new URL(explorerOrigin);
    return parsed.protocol === 'https:' && !parsed.username && !parsed.password && parsed.origin === explorerOrigin.replace(/\/$/, '') ? parsed.origin : null;
  } catch { return null; }
}

export function MarketTradeFeed({ pool, token, rows, watermark, contiguous, explorerOrigin }: { pool: TradeFeedPool | null; token: TradeFeedToken | null; rows: readonly Trade[] | null; watermark: Watermark | null; contiguous: boolean; explorerOrigin?: string; }) {
  const [filter, setFilter] = useState<SideFilter>('all');
  const [start, setStart] = useState(0);
  const [newItems, setNewItems] = useState(0);
  const scroller = useRef<HTMLDivElement>(null);
  const previous = useRef<string[]>([]);
  useEffect(() => {
    const restore = () => { const side = new URL(window.location.href).searchParams.get('side'); setFilter(side === 'buy' || side === 'sell' ? side : 'all'); setStart(0); setNewItems(0); if (scroller.current) scroller.current.scrollTop = 0; };
    restore(); window.addEventListener('popstate', restore); window.addEventListener('kindle:restore-navigation', restore);
    return () => { window.removeEventListener('popstate', restore); window.removeEventListener('kindle:restore-navigation', restore); };
  }, []);
  const identities = rows ? orderedTrades(rows).filter(row => filter === 'all' || row.side === filter).map(row => `${row.position.chainId}:${row.position.transactionHash.toLowerCase()}:${row.position.logIndex}`) : [];
  const identityKey = identities.join('|');
  const latest = useRef(identities);
  latest.current = identities;
  useLayoutEffect(() => {
    const identities = latest.current;
    const element = scroller.current;
    const old = previous.current;
    if (element && old.length && element.scrollTop > 0) {
      const anchor = old[Math.floor(element.scrollTop / rowHeight)];
      const nextIndex = anchor ? identities.indexOf(anchor) : -1;
      if (nextIndex >= 0) { const previousIndex = old.indexOf(anchor); const delta = nextIndex - previousIndex; if (delta !== 0) { element.scrollTop = Math.max(0, element.scrollTop + delta * rowHeight); setStart(Math.floor(element.scrollTop / rowHeight)); if (delta > 0) setNewItems(value => Math.min(1000, value + delta)); } }
      else { element.scrollTop = 0; setStart(0); setNewItems(0); }
    }
    previous.current = identities;
  }, [identityKey]);
  if (!pool || !token || !rows) return <section aria-label="Trade history"><EmptyState icon={ArrowLeftRight} title="Loading trades..." body="Trades show up here in a moment." /></section>;
  if (pool.tokenAddress.toLowerCase() !== token.address.toLowerCase() || rows.some(row => row.poolAddress.toLowerCase() !== pool.poolAddress.toLowerCase() || row.position.chainId !== pool.chainId)) return <section aria-label="Trade history" role="alert" className="p-4 text-sm text-danger">Trades can&apos;t load right now. Try again in a moment.</section>;
  if (watermark?.integrity === 'quarantined') return <section aria-label="Trade history" role="alert" className="p-4 text-sm text-danger">Trades can&apos;t load right now. Try again in a moment.</section>;
  const origin = explorerOriginFrom(explorerOrigin);
  const ordered = orderedTrades(rows);
  const matching = ordered.filter(row => filter === 'all' || row.side === filter);
  const visible = matching.slice(start, start + 100);
  const decimals = token.decimalsQuality === 'verified' ? token.decimals : null;
  const reset = () => { setStart(0); setNewItems(0); if (scroller.current) scroller.current.scrollTop = 0; };
  if (ordered.length === 0) return <section data-market-list="fills" data-market-history-size={0} aria-label="Trade history"><EmptyState icon={ArrowLeftRight} title="No trades yet" body="Trades appear here as soon as someone buys or sells this coin." /></section>;
  return <section data-market-list="fills" data-market-history-size={ordered.length} data-market-filter={filter} aria-label="Trade history" className="flex flex-col gap-3 text-text">
    <div className="flex flex-wrap items-center gap-2">
      <div role="group" aria-label="Trade type" className="flex items-center gap-2">
        {sideFilters.map(option => <Chip key={option.value} pressed={option.value === filter} onClick={() => { setFilter(option.value); reset(); const url = new URL(window.location.href); url.searchParams.set('side', option.value); window.history.replaceState(window.history.state, '', url); }}>{option.label}</Chip>)}
      </div>
      {newItems > 0 && <Chip pressed={false} onClick={reset} className="bg-accent-soft text-accent hover:bg-accent-soft-strong">Show {newItems} new trades</Chip>}
      <p role="status" className="ml-auto text-xs text-text-faint">{contiguous ? 'Live' : 'Catching up...'}</p>
    </div>
    {matching.length ? <div data-market-scroller="fills" ref={scroller} className="max-h-[480px] overflow-auto" style={{ overflowAnchor: 'none' }} onScroll={event => setStart(Math.min(Math.max(0, matching.length - 1), Math.floor(event.currentTarget.scrollTop / rowHeight)))}>
      <table className="w-full min-w-[720px] text-sm [&_td]:whitespace-nowrap [&_td]:px-3">
        <thead className="sticky top-0 z-[1] bg-surface-1"><tr>
          <SortHeader label="Block" />
          <SortHeader label="Type" />
          <SortHeader label="USDL" align="right" />
          <SortHeader label="Amount" align="right" />
          <SortHeader label="Price" align="right" />
          <SortHeader label="Account" />
          <SortHeader label="Transaction" align="right" />
        </tr></thead>
        <tbody>
          {start > 0 && <tr aria-hidden="true"><td colSpan={7} className="p-0" style={{ height: start * rowHeight }} /></tr>}
          {visible.map(row => {
            const key = `${row.position.chainId}:${row.position.transactionHash.toLowerCase()}:${row.position.logIndex}`;
            const href = origin && /^0x[0-9a-fA-F]{64}$/.test(row.position.transactionHash) ? `${origin}/tx/${row.position.transactionHash}` : null;
            const recipient = row.recipientAttribution === 'unavailable' || row.recipient === null ? null : shortAddress(row.recipient);
            return <tr data-market-trade-key={key} style={{ height: rowHeight }} key={key} className="bg-surface-1 transition-colors hover:bg-surface-hover">
              <td data-numeric className="font-mono text-xs tabular-nums text-text-muted">{row.position.height}</td>
              <td className={row.side === 'buy' ? 'font-bold capitalize text-success' : 'font-bold capitalize text-danger'}>{row.side}</td>
              <td data-numeric className="text-right tabular-nums text-text">{formatUsdl(row.usdlAmountRaw, { unit: false })}</td>
              <td data-numeric className="text-right tabular-nums text-text">{decimals === null ? <span className="text-text-faint">—</span> : formatTokenAmount(row.tokenAmountRaw, decimals)}</td>
              <td data-numeric className="text-right tabular-nums text-text-muted">{decimals === null ? <span className="text-text-faint">—</span> : exactRatio((BigInt(row.usdlAmountRaw) * 10n ** BigInt(decimals)).toString(), (BigInt(row.tokenAmountRaw) * 1000000n).toString())}</td>
              <td className="min-w-0">
                <span className="flex flex-col leading-tight">
                  <span className="font-mono text-xs text-text" title={row.sender}>{shortAddress(row.sender)}</span>
                  {recipient ? <span className="font-mono text-2xs text-text-faint">to {recipient}</span> : null}
                </span>
              </td>
              <td className="text-right">
                <span className="inline-flex flex-col items-end leading-tight">
                  {href ? <a href={href} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 rounded-xs font-mono text-xs text-accent hover:underline" title={row.position.transactionHash}>{shortAddress(row.position.transactionHash, 6, 4)}<ExternalLink aria-hidden="true" className="size-3" /><span className="sr-only">, opens in a new tab</span></a> : <span className="font-mono text-xs text-text-muted" title={row.position.transactionHash}>{shortAddress(row.position.transactionHash, 6, 4)}</span>}
                </span>
              </td>
            </tr>;
          })}
          {start + visible.length < matching.length && <tr aria-hidden="true"><td colSpan={7} className="p-0" style={{ height: (matching.length - start - visible.length) * rowHeight }} /></tr>}
        </tbody>
      </table>
    </div> : <EmptyState icon={Filter} announce title={filter === 'buy' ? 'No buys yet' : filter === 'sell' ? 'No sells yet' : 'No trades yet'} body={filter === 'all' ? 'Trades appear here as soon as someone buys or sells this coin.' : 'Switch to All to see every trade.'} />}
    {ordered.length > 100 && <p className="text-xs text-text-faint">Showing 100 trades at a time</p>}
  </section>;
}
