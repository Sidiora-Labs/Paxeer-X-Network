'use client';

import { useMemo, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { Globe, Link as LinkIcon, MessageCircle, Send, Sprout, Users } from 'lucide-react';
import { useExploreRows, type ExploreRow, type ExploreRowsState } from '@/widgets/explore/use-explore-rows';
import type { TokenMetadata } from '@/widgets/home/types';
import { safeSocialUrl } from '@/widgets/trading/token-description';
import { AgeChip, Avatar, Button, EmptyState, ErrorState, LoadMore, PercentPill, SegmentedControl, StageBadge, Tooltip, UnavailableState } from '@/ui/primitives';
import { TableRowsSkeleton } from '@/ui/shared/skeletons';
import { terminalPoolPath } from '../sub-nav';
import { formatCount, formatUsdl } from '@/utils/format';
import { userFacingMessage } from '@/core/user-facing-error';
import { StatDash } from '@/ui/shared/stat-value';
import { formatShare } from '@/core/market/pool-stats-display';

export type TrenchTimeframe = '1m' | '5m' | '30m' | '1h';

export const trenchTimeframes: readonly { value: TrenchTimeframe; label: string }[] = [
  { value: '1m', label: '1M' },
  { value: '5m', label: '5M' },
  { value: '30m', label: '30M' },
  { value: '1h', label: '1H' },
];

export interface WindowChange { value: number | null; reason: string | null }

const unservedWindow: Record<'1m' | '30m', string> = {
  '1m': 'A 1-minute change is not available. Choose 5m or 1h.',
  '30m': 'A 30-minute change is not available. Choose 5m or 1h.',
};

/**
 * Percent change for the selected window from the row's v2 statistics:
 * 5M and 1H read PoolStats.priceChangeBps5m / priceChangeBps1h, shown only on
 * a complete baseline (otherwise the baseline's reason, such as not enough
 * trading history). 1M and 30M have no v2 source at all.
 */
export function windowChange(row: Pick<ExploreRow, 'changes'>, timeframe: TrenchTimeframe): WindowChange {
  if (timeframe === '1m' || timeframe === '30m') return { value: null, reason: unservedWindow[timeframe] };
  const change = row.changes[timeframe];
  return change.value !== null && Number.isFinite(change.value) ? { value: change.value, reason: null } : { value: null, reason: change.reason ?? `No ${timeframe} change yet` };
}

/**
 * Served evidence for graduation. The pool contract, v2 PoolStats
 * (IF_STATS_BATCH) and the pool resolver were read for
 * a graduation, bonding-curve progress or DEX-migration field: none exists
 * (pools trade on one virtual-reserve curve for their whole life), so the two
 * graduation columns are unavailable rather than guessed.
 */
export const graduationEvidence = {
  served: false,
  operation: 'IF_STATS_BATCH PoolStats.bondingCurveProgress',
  message: 'Graduation progress is not tracked for coins yet, so no pair is placed here.',
} as const;

export interface TrenchBuckets {
  newPairs: ExploreRow[];
  aboutToGraduate: ExploreRow[] | null;
  graduated: ExploreRow[] | null;
}

/** Place rows from the 'new' ranking into the three trench columns; graduation buckets stay null without served evidence. */
export function trenchBuckets(rows: readonly ExploreRow[]): TrenchBuckets {
  if (!graduationEvidence.served) return { newPairs: [...rows], aboutToGraduate: null, graduated: null };
  return { newPairs: [...rows], aboutToGraduate: [], graduated: [] };
}

interface SocialLink { kind: 'website' | 'twitter' | 'telegram' | 'discord'; url: string }

export function pairSocials(metadata: TokenMetadata | null | undefined): SocialLink[] {
  const socials = metadata?.socials;
  if (!socials) return [];
  const out: SocialLink[] = [];
  for (const kind of ['website', 'twitter', 'telegram', 'discord'] as const) {
    const raw = socials[kind];
    if (!raw) continue;
    const url = safeSocialUrl(raw);
    if (url) out.push({ kind, url });
  }
  return out;
}

const socialIcon = { website: Globe, twitter: LinkIcon, telegram: Send, discord: MessageCircle } as const;
const socialLabel = { website: 'Website', twitter: 'X profile', telegram: 'Telegram', discord: 'Discord' } as const;

interface PairRowProps {
  row: ExploreRow;
  metadata: TokenMetadata | null;
  timeframe: TrenchTimeframe;
}

function PairRow({ row, metadata, timeframe }: PairRowProps) {
  const change = windowChange(row, timeframe);
  const socials = pairSocials(metadata);
  return (
    <li className="relative flex h-11 items-center gap-2 bg-surface-1 px-2 transition-colors duration-fast hover:bg-surface-hover">
      <Link href={terminalPoolPath(row.poolAddress)} aria-label={`${row.symbol} ${row.name}`} className="absolute inset-0 rounded-sm focus-visible:outline-2 focus-visible:outline-offset-[-2px] focus-visible:outline-focus" />
      <Avatar src={row.image} name={row.symbol} size="md" decorative badge={row.stage ? <StageBadge stage={row.stage} size={14} /> : undefined} />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <p className="flex min-w-0 items-baseline gap-1.5 text-sm">
          <span className="truncate font-bold text-text">{row.symbol}</span>
          <span className="truncate text-xs text-text-muted" title={row.name}>{row.name}</span>
        </p>
        <div className="flex min-w-0 items-center gap-2 text-xs text-text-faint">
          <AgeChip iso={row.createdAtMs} className="font-bold text-accent" />
          {row.holders !== null ? (
            <span className="inline-flex items-center gap-0.5 tabular-nums" title="Holders">
              <Users aria-hidden="true" className="size-3" />
              {formatCount(row.holders)}
              <span className="sr-only"> holders</span>
            </span>
          ) : null}
          {row.top10Share !== null ? <span className="tabular-nums" title="Share of supply held by the ten largest holders"><span className="sr-only">Top 10 holders hold </span><span aria-hidden="true">T10 </span>{formatShare(row.top10Share)}</span> : null}
          {row.devShare !== null ? <span className="tabular-nums" title="How much of the supply the creator holds"><span className="sr-only">Creator holds </span><span aria-hidden="true">Creator </span>{formatShare(row.devShare)}</span> : null}
          {socials.map(social => {
            const Icon = socialIcon[social.kind];
            return (
              <a key={social.kind} href={social.url} target="_blank" rel="noopener noreferrer" aria-label={`${socialLabel[social.kind]} for ${row.symbol}`} className="relative z-10 inline-flex rounded-xs text-text-faint hover:text-text focus-visible:outline-2 focus-visible:outline-focus">
                <Icon aria-hidden="true" className="size-3" />
              </a>
            );
          })}
        </div>
      </div>
      <div className="flex shrink-0 flex-col items-end gap-0.5">
        {row.mcapRaw === null
          ? <StatDash reason={row.mcapReason ?? 'Market cap is not available.'} subject="Market cap" className="text-sm" />
          : <span data-numeric title={row.circulatingMcapRaw ? `Circulating market cap ${formatUsdl(row.circulatingMcapRaw, { compact: true })}` : undefined} className="text-sm font-bold tabular-nums text-text">{formatUsdl(row.mcapRaw, { compact: true, unit: false })}</span>}
        {change.reason ? (
          <Tooltip content={change.reason} align="end">
            <span tabIndex={0} className="relative z-10 text-xs text-text-faint" aria-label={`Change not available yet. ${change.reason}`}>-</span>
          </Tooltip>
        ) : (
          <PercentPill value={change.value} variant="text" />
        )}
      </div>
    </li>
  );
}

interface TrenchColumnProps {
  title: string;
  count: number | null;
  control?: ReactNode;
  children: ReactNode;
}

function TrenchColumn({ title, count, control, children }: TrenchColumnProps) {
  return (
    <section aria-label={title} className="flex min-h-0 flex-col overflow-hidden rounded-md bg-surface-1">
      <header className="flex h-8 shrink-0 items-center justify-between gap-2 px-2">
        <h2 className="flex items-center gap-1.5 text-sm font-bold text-text">
          {title}
          {count !== null ? <span data-numeric className="text-xs font-semibold tabular-nums text-text-faint">{formatCount(count)}</span> : null}
        </h2>
        {control}
      </header>
      <div className="flex h-6 shrink-0 items-center justify-between px-2 text-2xs font-semibold uppercase text-text-faint">
        <span>Pair info</span>
        <span>Market cap</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">{children}</div>
    </section>
  );
}

function TimeframeControl({ value, onChange, label }: { value: TrenchTimeframe; onChange(next: TrenchTimeframe): void; label: string }) {
  return (
    <SegmentedControl<TrenchTimeframe>
      label={label}
      size="sm"
      value={value}
      onChange={onChange}
      options={trenchTimeframes.map(option => ({ value: option.value, label: option.label }))}
      className="text-xs"
    />
  );
}

function NewPairsBody({ state, timeframe }: { state: ExploreRowsState; timeframe: TrenchTimeframe }) {
  const buckets = useMemo(() => trenchBuckets(state.rows), [state.rows]);
  const metadata = state.metadata.state === 'ready' ? state.metadata.data : {};
  const ranking = state.ranking;
  if (ranking.state === 'loading') return <><span className="sr-only" role="status">Loading new pairs</span><TableRowsSkeleton rows={8} columns={3} /></>;
  if (ranking.state === 'unsupported') return <UnavailableState title="New pairs" operation={ranking.operation} status={ranking.status} onRetry={state.reload} />;
  if (ranking.state === 'error') return <ErrorState title="New pairs can't load right now" message={userFacingMessage(ranking.error)} onRetry={state.reload} variant="inline" />;
  if (buckets.newPairs.length === 0) return <EmptyState icon={Sprout} title="No new coins yet" body="New launches appear here as soon as they are created." action={<Button variant="secondary" size="sm" href="/create">Create coin</Button>} />;
  return (
    <>
      <ul className="flex flex-col gap-0.5 bg-canvas" aria-label="New pairs">
        {buckets.newPairs.map(row => (
          <PairRow key={row.poolAddress} row={row} timeframe={timeframe} metadata={row.tokenAddress ? metadata[row.tokenAddress] ?? null : null} />
        ))}
      </ul>
      {state.hasMore ? <LoadMore onLoadMore={state.loadMore} hasMore={state.hasMore} loading={state.loadingMore} error={state.moreError !== null} auto={false} className="py-2" /> : null}
    </>
  );
}

function NewPairsColumn() {
  const [timeframe, setTimeframe] = useState<TrenchTimeframe>('1h');
  const state = useExploreRows('new');
  const count = state.ranking.state === 'ready' ? state.rows.length : null;
  return (
    <TrenchColumn title="New pairs" count={count} control={<TimeframeControl label="New pairs change window" value={timeframe} onChange={setTimeframe} />}>
      <NewPairsBody state={state} timeframe={timeframe} />
    </TrenchColumn>
  );
}

function GraduationColumn({ title }: { title: string }) {
  return (
    <TrenchColumn title={title} count={null}>
      <UnavailableState title={title} operation={graduationEvidence.operation} status={null} message={graduationEvidence.message} />
    </TrenchColumn>
  );
}

/** Trenches board: new pairs from the 'new' ranking; graduation columns only from served evidence. */
export default function TerminalTrenchesView() {
  return (
    <div className="grid h-full min-h-0 grid-cols-1 gap-1 overflow-y-auto bg-canvas lg:grid-cols-3 lg:overflow-hidden">
      <NewPairsColumn />
      <GraduationColumn title="About to graduate" />
      <GraduationColumn title="Graduated" />
    </div>
  );
}
