'use client';
import { useRef, type ReactNode } from 'react';
import Link from 'next/link';
import { Star } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useSparkline } from '@/core/market/use-sparkline';
import { AgeChip, Avatar, ProgressBar, Sparkline, StageBadge } from '@/ui/primitives';
import { StatChange, StatDash } from '@/ui/shared/stat-value';
import { formatCompactUsd, formatCount, formatUsdl } from '@/utils/format';
import { columnSpecs, densityRowClass, type ColumnId, type ColumnVisibility, type Density, type OptionalColumn } from './column-settings';
import type { ExploreRow as ExploreRowData } from './use-explore-rows';
import { useVisible } from './use-visible';

export const athUnavailableReason = "All-time high isn't tracked yet";

export function visibleColumns(columns: ColumnVisibility): ColumnId[] {
  return columnSpecs.filter(spec => !(spec.id in columns) || columns[spec.id as OptionalColumn]).map(spec => spec.id);
}

function alignClass(align: 'left' | 'right' | 'center'): string {
  return align === 'right' ? 'text-right' : align === 'center' ? 'text-center' : 'text-left';
}

interface StarButtonProps {
  name: string;
  starred: boolean;
  onToggle: () => void;
  className?: string;
}

function StarButton({ name, starred, onToggle, className }: StarButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={starred}
      aria-label={starred ? `Remove ${name} from watchlist` : `Add ${name} to watchlist`}
      onClick={event => { event.preventDefault(); event.stopPropagation(); onToggle(); }}
      className={cn(
        'relative z-10 inline-flex size-control-height-sm shrink-0 items-center justify-center rounded-sm transition-colors duration-fast hover:bg-surface-hover focus-visible:outline-2 focus-visible:outline-focus',
        starred ? 'text-accent' : 'text-text-faint hover:text-text',
        className,
      )}
    >
      <Star aria-hidden="true" className={cn('size-icon-sm', starred && 'fill-current')} />
    </button>
  );
}

interface CoinCellProps {
  row: ExploreRowData;
}

/** Rank, avatar, name and ticker. Fixed width below xl so long names truncate and the table scrolls instead. */
function CoinCell({ row }: CoinCellProps) {
  return (
    <div className="relative flex w-36 min-w-0 items-center gap-2.5 md:w-56 xl:w-auto xl:min-w-56">
      <span data-numeric className="w-5 shrink-0 text-right text-num-sm tabular-nums text-text-faint">
        <span className="sr-only">Rank </span>{row.rank}
      </span>
      <Avatar src={row.image} name={row.name} size="md" shape="round" decorative badge={row.stage ? <StageBadge stage={row.stage} size={14} decorative surface={1} /> : undefined} />
      <div className="min-w-0">
        <Link
          href={row.href}
          title={row.name}
          className="block truncate text-sm font-semibold text-text after:absolute after:inset-0 after:content-[''] focus-visible:outline-2 focus-visible:outline-focus"
        >
          {row.name}
          <span className="sr-only">{row.mcap === null ? ', no market cap yet' : `, market cap ${formatCompactUsd(row.mcap)}`}</span>
        </Link>
        <span className="block truncate text-xs text-text-faint" title={row.symbol}>${row.symbol}</span>
      </div>
    </div>
  );
}

export interface ExploreRowProps {
  row: ExploreRowData;
  columns: ColumnVisibility;
  density: Density;
  starred: boolean;
  onToggleStar: (poolAddress: string) => void;
  /** Alternate between rows; the only separation between rows. */
  tone: 'a' | 'b';
}

export function ExploreRow({ row, columns, density, starred, onToggleStar, tone }: ExploreRowProps) {
  const ref = useRef<HTMLTableRowElement | null>(null);
  const visible = useVisible(ref);
  const sparkline = useSparkline(visible && columns.graph ? row.poolAddress : null);

  const cells: Partial<Record<ColumnId, ReactNode>> = {
    coin: <CoinCell row={row} />,
    graph: sparkline.status === 'pending'
      ? <Sparkline data={[]} loading label="24h price" />
      : <Sparkline data={sparkline.data?.closes ?? []} label="24h price" />,
    mcap: row.mcap === null
      ? <StatDash reason={row.mcapReason ?? 'No market cap yet'} subject="Market cap" />
      : <span data-numeric title={row.circulatingMcapRaw ? `Circulating market cap ${formatUsdl(row.circulatingMcapRaw, { compact: true })}` : undefined} className="text-num-sm tabular-nums text-text">{formatCompactUsd(row.mcap)}</span>,
    ath: (
      <span title={athUnavailableReason} className="inline-flex items-center gap-2 text-xs text-text-faint">
        <ProgressBar value={null} label="share of all-time-high market cap" width={40} />
        <span>Not yet</span>
      </span>
    ),
    age: <AgeChip iso={row.createdAtMs} />,
    txns: <span data-numeric className={cn('text-num-sm tabular-nums', row.txns === null ? 'text-text-faint' : 'text-text')}>{formatCount(row.txns)}</span>,
    volume24h: <span data-numeric className={cn('text-num-sm tabular-nums', row.volume24h === null ? 'text-text-faint' : 'text-text')}>{formatCompactUsd(row.volume24h)}</span>,
    traders: <span data-numeric className={cn('text-num-sm tabular-nums', row.traders === null ? 'text-text-faint' : 'text-text')}>{formatCount(row.traders)}</span>,
    change1h: <StatChange change={row.changes['1h']} arrow />,
    change6h: <StatChange change={row.changes['6h']} arrow />,
    change24h: <StatChange change={row.changes['24h']} arrow />,
    star: <StarButton name={row.name} starred={starred} onToggle={() => onToggleStar(row.poolAddress)} />,
  };

  return (
    <tr ref={ref} className={cn('transition-colors duration-fast hover:bg-surface-hover', densityRowClass[density], tone === 'a' ? 'bg-surface-1' : 'bg-surface-2')}>
      {columnSpecs.map(spec => {
        if (spec.id in columns && !columns[spec.id as OptionalColumn]) return null;
        return (
          <td
            key={spec.id}
            data-numeric={spec.align === 'right' || undefined}
            className={cn('px-3 align-middle whitespace-nowrap', spec.id === 'coin' && 'relative', alignClass(spec.align), spec.responsive)}
          >
            {cells[spec.id]}
          </td>
        );
      })}
    </tr>
  );
}
