'use client';
import { Suspense } from 'react';
import { useSearchParams } from 'next/navigation';
import { TrendingUp } from 'lucide-react';
import { SourceNotice } from '@/ui/common/source-notice';
import { Button, EmptyState, Skeleton, StateNotice, UnavailableState } from '@/ui/primitives';
import { ExploreView } from '@/widgets/explore/explore-view';
import { presetFor, PresetTabs } from '@/widgets/explore/preset-tabs';
import { useExploreRows } from '@/widgets/explore/use-explore-rows';

function ExplorePage() {
  const params = useSearchParams();
  const preset = presetFor(params.get('preset'));
  const data = useExploreRows(preset.category);
  const notices = [<SourceNotice key="stats" label="Coin stats" state={data.stats} />, <SourceNotice key="details" label="Coin details" state={data.metadata} />];
  const tabs = <PresetTabs active={preset.id} />;

  const rankingFailed = data.ranking.state === 'unsupported' || data.ranking.state === 'error';
  /* A failed ranking keeps the toolbar and the table frame (headers, card) and puts a notice inside it. */
  let empty: React.ReactNode;
  if (data.ranking.state === 'unsupported') {
    empty = (
      <div data-ranking-state="unsupported" className="flex min-h-64 items-center justify-center p-6">
        <UnavailableState variant="chip" plural="auto" surface={1} wrap title="Coins" operation={data.ranking.operation} status={data.ranking.status} message={data.ranking.message} onRetry={data.reload} />
      </div>
    );
  } else if (data.ranking.state === 'error') {
    empty = (
      <div data-ranking-state="error" className="flex min-h-64 items-center justify-center p-6">
        <StateNotice kind="error" surface={1} wrap error={data.ranking.error} onRetry={data.ranking.retry}>
          {"Couldn't load coins right now"}
        </StateNotice>
      </div>
    );
  } else {
    empty = <EmptyState variant="panel" icon={TrendingUp} title="No coins yet" body="Coins show up here once they start trading" action={<Button variant="primary" size="sm" href="/create">Create coin</Button>} announce />;
  }

  const body = (
    <ExploreView
      rows={rankingFailed ? [] : data.rows}
      loading={rankingFailed ? false : data.loading}
      hasMore={rankingFailed ? false : data.hasMore}
      loadingMore={rankingFailed ? false : data.loadingMore}
      moreError={rankingFailed ? null : data.moreError}
      onLoadMore={data.loadMore}
      onRetryMore={data.retryMore}
      defaultSort={preset.defaultSort}
      sortResetKey={preset.id}
      sourceLabel="coins"
      leading={tabs}
      caption={`${preset.label} coins`}
      empty={empty}
    />
  );

  return (
    <div className="mx-auto flex w-full max-w-content-max-width flex-col gap-4 px-4 py-4 md:px-6">
      <h1 className="text-lg font-medium text-text">Explore coins</h1>
      {!rankingFailed && [data.stats, data.metadata].some(source => source.state === 'unsupported' || source.state === 'error') ? <div className="flex flex-col gap-2">{notices}</div> : null}
      {body}
    </div>
  );
}

function ExploreFallback() {
  return (
    <div className="mx-auto flex w-full max-w-content-max-width flex-col gap-4 px-4 py-4 md:px-6" aria-busy="true">
      <h1 className="text-lg font-medium text-text">Explore coins</h1>
      <Skeleton variant="rows" rows={8} label="coins" className="rounded-md bg-surface-1" />
    </div>
  );
}

export default function ExploreModule() {
  return (
    <Suspense fallback={<ExploreFallback />}>
      <ExplorePage />
    </Suspense>
  );
}
