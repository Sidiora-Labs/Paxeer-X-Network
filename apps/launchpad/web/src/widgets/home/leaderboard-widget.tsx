'use client';
import { useMemo, useState } from 'react';
import Link from 'next/link';
import { Trophy } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Avatar, EmptyState, ErrorState, RankBadge, SegmentedControl, SelectPill, UnavailableState } from '@/ui/primitives';
import { useLeaderboard, type LeaderboardCategory, type LeaderboardWindow } from '@/core/market/use-leaderboard';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { formatUsdl, shortAddress } from '@/utils/format';
import { RankRowSkeleton } from '@/ui/shared/skeletons';
import type { WidgetProps } from './widget-registry';
import { columnNoticeClass, columnToolbarClass } from './panel-state';

export const leaderboardCategories: { value: LeaderboardCategory; label: string }[] = [
  { value: 'trader', label: 'Traders' },
  { value: 'creator', label: 'Creators' },
  { value: 'volume', label: 'Volume' },
];

export const leaderboardWindows: { value: LeaderboardWindow; label: string }[] = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
];

/** Empty-snapshot copy per board; a successful empty board before the first trades is a normal state. */
export const leaderboardEmptyTitles: Record<LeaderboardCategory, string> = {
  trader: 'No traders yet',
  creator: 'No creators yet',
  volume: 'No volume yet',
};
export function leaderboardEmptyBody(window: LeaderboardWindow): string {
  return `Nothing in the last ${window} yet`;
}

export interface LeaderboardMetric {
  text: string;
  tone: 'success' | 'danger' | 'neutral';
}

/** Signed metric (PnL) when the entry carries one, otherwise the unsigned aggregate (volume, creations). */
export function leaderboardMetric(entry: { metricRaw: string; metricSigned?: string; quality: 'complete' | 'partial' | 'unavailable' }): LeaderboardMetric {
  if (entry.quality === 'unavailable') return { text: '—', tone: 'neutral' };
  const signed = entry.metricSigned ?? null;
  if (signed !== null) {
    const negative = signed.startsWith('-');
    return { text: formatUsdl(signed, { compact: true, unit: false, signed: true }), tone: negative ? 'danger' : 'success' };
  }
  return { text: formatUsdl(entry.metricRaw, { compact: true, unit: false }), tone: 'neutral' };
}

const toneClass: Record<LeaderboardMetric['tone'], string> = { success: 'text-success', danger: 'text-danger', neutral: 'text-text' };

/**
 * Leaderboard column (WP-09, features.csv 158-161): traders, creators and
 * volume by window from IF_LEADERBOARDS. The real 404 renders as an
 * unavailable state; rows link to the wallet profile.
 */
export function LeaderboardWidget({ filter }: WidgetProps) {
  const [category, setCategory] = useState<LeaderboardCategory>('trader');
  const [window, setWindow] = useState<LeaderboardWindow>('24h');
  const board = useLeaderboard(category, window, 50);
  const entries = useMemo(() => (board.state === 'ready' ? board.data.data.snapshot.entries : []), [board]);
  const wallets = useMemo(() => entries.map(entry => entry.wallet), [entries]);
  const identities = useWalletIdentities(wallets);
  const needle = filter.trim().toLowerCase();
  const visible = useMemo(() => entries.filter(entry => !needle || entry.wallet.toLowerCase().includes(needle) || identities.label(entry.wallet).toLowerCase().includes(needle)), [entries, needle, identities]);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className={columnToolbarClass}>
        <SegmentedControl label="Leaderboard" options={leaderboardCategories} value={category} onChange={setCategory} size="sm" shape="sm" compact />
        <SelectPill label="Window" className="ml-auto" options={leaderboardWindows} value={window} onChange={setWindow} size="sm" />
      </div>
      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
        {board.state === 'loading' ? (
          <RankRowSkeleton count={8} />
        ) : board.state === 'unsupported' ? (
          <UnavailableState variant="chip" plural="auto" surface={1} wrap className={columnNoticeClass} title="Leaderboard" operation={board.operation} status={board.status} message={board.status === 404 ? 'The leaderboard is coming soon' : board.message} />
        ) : board.state === 'error' ? (
          <ErrorState variant="chip" className={columnNoticeClass} message="Couldn't load the leaderboard" error={board.error} onRetry={board.retry} />
        ) : visible.length === 0 ? (
          <EmptyState icon={Trophy} title={needle ? 'No wallets match this filter' : leaderboardEmptyTitles[category]} body={needle ? `Nothing matches "${filter.trim()}"` : leaderboardEmptyBody(window)} />
        ) : (
          <ol className="flex flex-col">
            {visible.map(entry => {
              const identity = identities.identities[entry.wallet.toLowerCase()] ?? identities.identities[entry.wallet];
              const name = identity?.displayName ?? null;
              const metric = leaderboardMetric(entry);
              return (
                <li key={entry.wallet} className="relative flex h-table-row items-center gap-3 px-3 hover:bg-surface-hover">
                  <RankBadge rank={entry.rank} className="w-7 shrink-0" />
                  <Avatar src={identity?.avatarUrl ?? null} name={name ?? shortAddress(entry.wallet)} size="md" decorative />
                  <Link href={`/profile/${entry.wallet}`} className={cn('min-w-0 flex-1 truncate after:absolute after:inset-0 after:content-[\'\']', name ? 'text-md font-bold text-text' : 'font-mono text-sm text-text-muted')}>
                    {name ?? shortAddress(entry.wallet)}
                  </Link>
                  {entry.quality === 'partial' ? <span className="text-2xs text-text-faint" title="Still counting">partial</span> : null}
                  <span data-numeric className={cn('shrink-0 text-num-md font-bold tabular-nums', toneClass[metric.tone])}>{metric.text}</span>
                </li>
              );
            })}
          </ol>
        )}
      </div>
    </div>
  );
}

export default LeaderboardWidget;
