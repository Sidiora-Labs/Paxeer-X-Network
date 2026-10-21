'use client';
import { useMemo, type ReactNode } from 'react';
import { TrendingUp } from 'lucide-react';
import { Button, EmptyState, ErrorState, UnavailableState } from '@/ui/primitives';
import { SourceNotice } from '@/ui/common/source-notice';
import { ExploreView } from '@/widgets/explore/explore-view';
import { useExploreRows, type ExploreRow } from '@/widgets/explore/use-explore-rows';
import { terminalPoolPath } from '../sub-nav';
import { userFacingMessage } from '@/core/user-facing-error';

/** Explore rows with their links pointed at the terminal coin view instead of the coin page. */
export function toTerminalRows(rows: readonly ExploreRow[]): ExploreRow[] {
  return rows.map(row => ({ ...row, href: terminalPoolPath(row.poolAddress) }));
}


/**
 * Full-width trending pairs table inside the terminal: the IF_RANKINGS
 * trending snapshot enriched exactly as Explore does (IF_STATS_BATCH, pool
 * listings, metadata batch), with every row opening /terminal/<pool>.
 */
export default function TerminalTrendingView() {
  const data = useExploreRows('trending');
  const rows = useMemo(() => toTerminalRows(data.rows), [data.rows]);

  let body: ReactNode;
  if (data.ranking.state === 'unsupported') {
    body = <div className="rounded-md bg-surface-1"><UnavailableState title="Trending" operation={data.ranking.operation} status={data.ranking.status} message={data.ranking.message} onRetry={data.reload} /></div>;
  } else if (data.ranking.state === 'error') {
    body = <div className="rounded-md bg-surface-1"><ErrorState title="Trending can't load right now" message={userFacingMessage(data.ranking.error)} onRetry={data.ranking.retry} /></div>;
  } else {
    body = (
      <ExploreView
        rows={rows}
        loading={data.loading}
        hasMore={data.hasMore}
        loadingMore={data.loadingMore}
        moreError={data.moreError}
        onLoadMore={data.loadMore}
        onRetryMore={data.retryMore}
        defaultSort={null}
        sortResetKey="terminal-trending"
        sourceLabel="trending"
        leading={<h2 className="text-sm font-bold text-text">Trending pairs</h2>}
        caption="Trending pairs"
        empty={<EmptyState variant="panel" icon={TrendingUp} title="No coins trending yet" body="Rankings appear as soon as coins trade." action={<Button variant="secondary" size="sm" href="/create">Create coin</Button>} announce />}
      />
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-2 py-1">
      <SourceNotice label="Coin stats" state={data.stats} />
      <SourceNotice label="Coin details" state={data.metadata} />
      {body}
    </div>
  );
}
