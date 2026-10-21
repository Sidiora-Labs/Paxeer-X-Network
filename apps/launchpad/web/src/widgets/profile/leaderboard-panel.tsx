'use client';

import { useId, useState, useSyncExternalStore } from 'react';
import Link from 'next/link';
import { useQueryClient } from '@tanstack/react-query';
import { Trophy } from 'lucide-react';
import { cn } from '@/lib/utils';
import { queryKeys } from '@/core/query-keys';
import { leaderboardLimit, useLeaderboard, type LeaderboardCategory, type LeaderboardWindow, type LeaderboardsResponse } from '@/core/market/use-leaderboard';
import { useWalletIdentities, type WalletIdentity } from '@/core/market/use-wallet-identities';
import type { Loadable } from '@/core/market/unsupported';
import { Avatar, EmptyState, ErrorState, RankBadge, SelectPill, Skeleton, TabStrip, UnavailableState, type SelectOption, type TabItem } from '@/ui/primitives';
import { formatUsdl, relativeAge, shortAddress } from '@/utils/format';
import { accountSession, subscribeAccount } from './account-transport';
import { useProfileIssue } from './profile-issues';

export type LeaderboardPanelTab = 'traders' | 'rewards';
export type LeaderboardMetric = 'profit' | 'volume';
export type LeaderboardEntry = LeaderboardsResponse['data']['snapshot']['entries'][number];

/** Metric labels map onto the contract categories; Profit is the trader PnL board, Volume the traded-volume board. */
export const leaderboardMetricCategory: Record<LeaderboardMetric, LeaderboardCategory> = { profit: 'trader', volume: 'volume' };
/** Rows the rail requests; the contract caps a page at `leaderboardLimit`. */
export const profileLeaderboardRows = 25;

const tabs: TabItem<LeaderboardPanelTab>[] = [
  { id: 'traders', label: 'Top traders' },
  { id: 'rewards', label: 'Top rewards' },
];
const metricOptions: SelectOption<LeaderboardMetric>[] = [
  { value: 'profit', label: 'Profit' },
  { value: 'volume', label: 'Volume' },
];
const windowOptions: SelectOption<LeaderboardWindow>[] = [
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
  { value: '30d', label: '30d' },
];

const noSession = () => null;

export interface LeaderboardPanelProps {
  /** Rows requested per board (1 to `leaderboardLimit`). */
  limit?: number;
  initialMetric?: LeaderboardMetric;
  initialWindow?: LeaderboardWindow;
  className?: string;
}

/** Value column text; `unavailable` entries carry no trustworthy metric and render a dash. */
export function leaderboardValue(entry: LeaderboardEntry, metric: LeaderboardMetric): string | null {
  if (entry.quality === 'unavailable') return null;
  return formatUsdl(entry.metricRaw, { signed: metric === 'profit' });
}

/** Short wallet without the 0x prefix so the initials fallback reads as two hex digits. */
function avatarName(identity: WalletIdentity | undefined, wallet: string): string {
  return identity?.displayName ?? wallet.slice(2, 6);
}

interface LeaderboardRowProps {
  entry: LeaderboardEntry;
  identity: WalletIdentity | undefined;
  metric: LeaderboardMetric;
  self: boolean;
}

function LeaderboardRow({ entry, identity, metric, self }: LeaderboardRowProps) {
  const wallet = entry.wallet.toLowerCase();
  const name = identity?.displayName ?? null;
  const label = name ?? identity?.shortAddress ?? shortAddress(wallet);
  const value = leaderboardValue(entry, metric);
  return (
    <li>
      <Link
        href={`/profile/${wallet}`}
        aria-current={self ? 'true' : undefined}
        className={cn(
          'flex h-12 items-center gap-3 rounded-sm px-2 transition-colors duration-fast hover:bg-surface-hover',
          self && 'bg-accent-soft',
        )}
      >
        <span className="flex w-8 shrink-0 justify-center">
          <RankBadge rank={entry.rank} />
        </span>
        <Avatar src={identity?.avatarUrl ?? null} name={avatarName(identity, wallet)} size="md" decorative loading={identity?.resolution === 'pending'} />
        <span className="flex min-w-0 flex-1 items-center gap-1.5">
          <span className={cn('truncate text-sm font-bold', name ? 'text-text' : 'font-mono text-text-muted')}>{label}</span>
          {self ? <span className="shrink-0 rounded-pill bg-accent-soft-strong px-1.5 text-2xs font-bold text-accent">You</span> : null}
          {entry.quality === 'partial' ? <span className="shrink-0 text-2xs text-text-faint">partial</span> : null}
        </span>
        <span
          data-numeric
          className={cn('shrink-0 text-num-md font-bold tabular-nums', value === null ? 'text-text-faint' : metric === 'profit' ? 'text-success' : 'text-text')}
        >
          {value ?? (
            <>
              <span className="sr-only">No value yet</span>
              <span aria-hidden="true">{'—'}</span>
            </>
          )}
        </span>
      </Link>
    </li>
  );
}

interface LeaderboardBodyProps {
  board: Loadable<LeaderboardsResponse>;
  identities: Record<string, WalletIdentity>;
  metric: LeaderboardMetric;
  viewer: string | null;
  onRetry: () => void;
}

/** Static row frames for a board that could not load; the page notice carries the reason. */
function PlaceholderRows({ rows }: { rows: number }) {
  return (
    <ol aria-hidden="true" data-leaderboard-placeholder className="flex flex-col gap-0.5">
      {Array.from({ length: rows }).map((_, index) => (
        <li key={index} className="flex h-11 items-center gap-3 px-3">
          <span className="w-5 shrink-0 text-center text-xs text-text-faint">{index + 1}</span>
          <span className="size-avatar-md shrink-0 rounded-pill bg-surface-2" />
          <span className="h-3 w-24 rounded-xs bg-surface-2" />
          <span className="ml-auto text-sm text-text-faint">—</span>
        </li>
      ))}
    </ol>
  );
}

function LeaderboardBody({ board, identities, metric, viewer, onRetry }: LeaderboardBodyProps) {
  const collected = useProfileIssue('leaderboard', board.state === 'error' ? { label: 'Leaderboard', error: board.error, retry: board.retry } : null);
  if (board.state === 'loading') return <Skeleton variant="rows" rows={8} label="leaderboard" />;
  if (board.state === 'unsupported') {
    return <UnavailableState title="Leaderboard" operation={board.operation} status={board.status} message="Check back in a bit" onRetry={onRetry} />;
  }
  if (board.state === 'error' && collected) return <PlaceholderRows rows={12} />;
  if (board.state === 'error') return <ErrorState title="Can't load leaderboard right now" message="Check your connection and try again" onRetry={board.retry} />;
  const entries = board.data.data.snapshot.entries;
  if (entries.length === 0) {
    return <EmptyState icon={Trophy} title="No traders ranked yet" body="Rankings show up once people start trading" />;
  }
  return (
    <ol className="flex flex-col gap-0.5" aria-label={`Top ${metric === 'profit' ? 'traders by profit' : 'wallets by volume'}`}>
      {entries.map(entry => {
        const wallet = entry.wallet.toLowerCase();
        return <LeaderboardRow key={wallet} entry={entry} identity={identities[wallet]} metric={metric} self={viewer === wallet} />;
      })}
    </ol>
  );
}

/**
 * Profile left rail: ranked wallets from IF_LEADERBOARDS. The board, metric
 * and window controls drive the real request; a 404/501/503 from core/api
 * arrives as the Loadable's unsupported marker and renders the unavailable
 * state with a retry. Top rewards has no contract category, so that tab
 * explains itself without issuing a request.
 */
export function LeaderboardPanel({ limit = profileLeaderboardRows, initialMetric = 'profit', initialWindow = '7d', className }: LeaderboardPanelProps) {
  const idPrefix = `leaderboard${useId()}`;
  const [tab, setTab] = useState<LeaderboardPanelTab>('traders');
  const [metric, setMetric] = useState<LeaderboardMetric>(initialMetric);
  const [period, setPeriod] = useState<LeaderboardWindow>(initialWindow);
  const rows = Math.min(limit, leaderboardLimit);
  const category = leaderboardMetricCategory[metric];
  const board = useLeaderboard(category, period, rows);
  const queryClient = useQueryClient();
  const entries = board.state === 'ready' ? board.data.data.snapshot.entries : [];
  const { identities } = useWalletIdentities(entries.map(entry => entry.wallet));
  const viewer = useSyncExternalStore(subscribeAccount, accountSession, noSession)?.session.wallet.toLowerCase() ?? null;

  const retry = () => { void queryClient.invalidateQueries({ queryKey: queryKeys.leaderboard(category, period, rows) }); };
  const panelId = (id: LeaderboardPanelTab) => `${idPrefix}-panel-${id}`;
  const tabId = (id: LeaderboardPanelTab) => `${idPrefix}-tab-${id}`;

  return (
    <section aria-label="Leaderboard" className={cn('flex flex-col gap-2 rounded-lg bg-surface-1 p-2', className)}>
      <TabStrip tabs={tabs} active={tab} onChange={setTab} label="Leaderboard" idPrefix={idPrefix} />
      {tab === 'traders' ? (
        <div role="tabpanel" id={panelId('traders')} aria-labelledby={tabId('traders')} className="flex flex-col gap-2">
          <div className="flex flex-wrap items-center gap-2 px-1">
            <SelectPill options={metricOptions} value={metric} onChange={setMetric} label="Metric" />
            <SelectPill options={windowOptions} value={period} onChange={setPeriod} label="Window" />
            {board.state === 'ready' ? (
              <span className="ml-auto text-2xs text-text-faint">
                Updated <time dateTime={board.data.generatedAt}>{relativeAge(board.data.generatedAt)}</time> ago
              </span>
            ) : null}
          </div>
          <LeaderboardBody board={board} identities={identities} metric={metric} viewer={viewer} onRetry={retry} />
        </div>
      ) : (
        <div role="tabpanel" id={panelId('rewards')} aria-labelledby={tabId('rewards')}>
          <UnavailableState
            title="Top rewards"
            plural
            operation="IF_LEADERBOARDS"
            status={null}
            message="Check back soon"
          />
        </div>
      )}
    </section>
  );
}

export default LeaderboardPanel;
