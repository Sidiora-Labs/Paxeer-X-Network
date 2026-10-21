'use client';
import { useMemo } from 'react';
import { Star } from 'lucide-react';
import { SourceNotice } from '@/ui/common/source-notice';
import { useWatchlistRows } from '@/core/market/use-watchlist-rows';
import { Button, EmptyState, NoticeBar } from '@/ui/primitives';
import { ExploreView } from '@/widgets/explore/explore-view';
import { buildExploreRow, type ExploreRow } from '@/widgets/explore/use-explore-rows';

export default function WatchlistModule() {
  const watchlist = useWatchlistRows();
  const rows = useMemo<ExploreRow[]>(() => {
    const now = Date.now();
    return watchlist.rows.map((row, index) => buildExploreRow({ poolAddress: row.poolAddress, rank: index + 1, stats: row.stats, listing: row.listing, metadata: row.metadata, now }));
  }, [watchlist.rows]);
  const notices = [<SourceNotice key="stats" label="Coin stats" state={watchlist.stats} />, <SourceNotice key="details" label="Coin details" state={watchlist.metadata} />];
  const loading = watchlist.loading && watchlist.pools.length === 0;

  return (
    <div className="mx-auto flex w-full max-w-content-max-width flex-col gap-4 px-4 py-4 md:px-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h1 className="text-xl font-extrabold text-text">Watchlist</h1>
        <p className="text-sm text-text-muted" role="status">{watchlist.pools.length === 1 ? '1 starred coin' : `${watchlist.pools.length} starred coins`}</p>
      </div>
      {watchlist.isLocal ? (
        <NoticeBar tone="info">Sign in to keep your watchlist on every device</NoticeBar>
      ) : null}
      {[watchlist.stats, watchlist.metadata].some(source => source.state === 'unsupported' || source.state === 'error') ? <div className="flex flex-col gap-2">{notices}</div> : null}
      <ExploreView
        rows={rows}
        loading={loading}
        hasMore={false}
        loadingMore={false}
        moreError={null}
        onLoadMore={() => undefined}
        onRetryMore={() => undefined}
        defaultSort={null}
        sortResetKey="watchlist"
        sourceLabel="coins"
        caption="Starred coins"
        empty={(
          <EmptyState
            variant="panel"
            icon={Star}
            announce
            title="Your watchlist is empty"
            body="Star coins to keep them here."
            action={<Button variant="primary" size="md" href="/explore">Browse coins</Button>}
          />
        )}
      />
    </div>
  );
}
