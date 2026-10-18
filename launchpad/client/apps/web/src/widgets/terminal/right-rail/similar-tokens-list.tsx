'use client';
import Link from 'next/link';
import { ArrowDownUp } from 'lucide-react';
import { AgeChip, Avatar, DropdownMenu, EmptyState, ErrorState, Skeleton, Tooltip, UnavailableState } from '@/ui/primitives';
import { formatUsdl } from '@/utils/format';
import { sortSimilar, useSimilarTokens, type SimilarRow, type SimilarSort } from './similar-tokens';
import { userFacingMessage } from '@/core/user-facing-error';

const sortLabels: Record<SimilarSort, string> = { liq: 'LIQ', vol: 'VOL', mc: 'MC' };

function Figure({ raw, unit, reason }: { raw: string | null; unit: string; reason: string }) {
  return (
    <span className="flex items-baseline justify-end gap-1 text-xs tabular-nums">
      {raw === null ? (
        <Tooltip content={reason}><span tabIndex={0} className="font-bold text-text-faint" aria-label={`${unit} not available yet: ${reason}`}>-</span></Tooltip>
      ) : <span className="font-bold text-text">{formatUsdl(raw, { compact: true, unit: false })}</span>}
      <span className="w-6 text-2xs text-text-faint">{unit}</span>
    </span>
  );
}

function Row({ row, liqReason }: { row: SimilarRow; liqReason: string }) {
  return (
    <li>
      <Link href={row.href} className="flex items-center gap-2 rounded-sm bg-surface-2 p-2 transition-colors duration-fast hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">
        <Avatar src={row.image} name={row.symbol} size="lg" shape="square" decorative />
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex min-w-0 items-baseline gap-1">
            <span className="truncate text-sm font-bold text-text">{row.symbol}</span>
            <span className="truncate text-xs text-text-muted">{row.name}</span>
          </span>
          {row.createdAtMs !== null ? <AgeChip iso={row.createdAtMs} compact /> : <span className="text-2xs text-text-faint">—</span>}
        </span>
        <span className="flex shrink-0 flex-col gap-0.5">
          <Figure raw={row.liquidityRaw} unit="Liq" reason={liqReason} />
          <Figure raw={row.volumeRaw} unit="Vol" reason="Volume is not available yet" />
          <Figure raw={row.mcapRaw} unit="MC" reason="Market cap is not available yet" />
        </span>
      </Link>
    </li>
  );
}

export function SimilarSortMenu({ sort, onChange }: { sort: SimilarSort; onChange: (sort: SimilarSort) => void }) {
  return (
    <DropdownMenu
      label={`Sort similar coins, currently ${sortLabels[sort]}`}
      size="sm"
      align="end"
      trigger={<span className="inline-flex items-center gap-1 text-xs font-bold">{sortLabels[sort]}<ArrowDownUp aria-hidden="true" className="size-3" /></span>}
      triggerClassName="h-6 rounded-pill bg-surface-2 px-2 text-text hover:bg-surface-hover"
      items={(Object.keys(sortLabels) as SimilarSort[]).map(value => ({ id: value, label: value === 'liq' ? 'Liquidity' : value === 'vol' ? '24h volume' : 'Market cap', selected: value === sort, onSelect: () => onChange(value) }))}
    />
  );
}

export function SimilarTokensList({ poolAddress, symbol, sort }: { poolAddress: string; symbol: string | null; sort: SimilarSort }) {
  const state = useSimilarTokens(poolAddress, symbol);
  if (!symbol) return <p className="text-xs text-text-faint">Loading...</p>;
  if (state.search.state === 'loading') return <Skeleton variant="rows" rows={4} label="similar coins" />;
  if (state.search.state === 'unsupported') return <UnavailableState variant="inline" title="Similar coins" operation={state.search.operation} status={state.search.status} />;
  if (state.search.state === 'error') return <ErrorState variant="inline" message={`Similar coins can't load right now. ${userFacingMessage(state.search.error)}`} onRetry={state.search.retry} />;
  if (state.rows.length === 0) return <EmptyState variant="inline" title="No similar coins yet" body={`No other coin uses the symbol ${symbol}.`} />;
  const liqReason = state.stats.state === 'unsupported'
    ? 'Liquidity is not available yet'
    : state.stats.state === 'error'
      ? `Liquidity can't load right now. ${userFacingMessage(state.stats.error)}`
      : state.stats.state === 'loading' ? 'Loading...' : 'Liquidity is not available yet';
  return (
    <ul aria-busy={state.enrichmentPending || undefined} className="flex flex-col gap-1">
      {sortSimilar(state.rows, sort).map(row => <Row key={row.poolAddress} row={row} liqReason={liqReason} />)}
    </ul>
  );
}
