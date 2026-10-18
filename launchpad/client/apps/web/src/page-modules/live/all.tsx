'use client';

import { Suspense, useMemo, useState } from 'react';
import Link from 'next/link';
import { ChevronLeft } from 'lucide-react';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { LoadMore, SegmentedControl } from '@/ui/primitives';
import { StreamCard, StreamCardSkeleton } from '@/widgets/live/stream-card';
import { LiveUnavailable } from '@/widgets/live/live-unavailable';
import { liveSortLabels, parseLiveSort, streamGridClass, type LiveSort } from '@/widgets/live/ranked-row';
import { rankByMarketCap, rankByViewers, useLiveDirectory, useLiveDirectoryContinuation, type LiveDirectoryEntry } from '@/widgets/live/use-live-directory';
import { DirectoryNotices, NobodyLive, directoryFailure } from './index';

export const liveAllPageSize = 24;

const sortOptions: { value: LiveSort; label: string }[] = [
  { value: 'viewers', label: liveSortLabels.viewers },
  { value: 'mcap', label: liveSortLabels.mcap },
];

function LiveAllView() {
  const params = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const sort = parseLiveSort(params.get('sort'));
  const [shown, setShown] = useState(liveAllPageSize);
  const directory = useLiveDirectory();
  const failure = directoryFailure(directory.state);
  const cursor = directory.state.state === 'ready' ? directory.state.data.nextCursor : null;
  const [start, setStart] = useState<string | null>(null);
  const more = useLiveDirectoryContinuation(start);
  const ranked = useMemo(() => {
    const seen = new Set(directory.entries.map(entry => entry.stream.id));
    const combined: LiveDirectoryEntry[] = [...directory.entries, ...more.entries.filter(entry => !seen.has(entry.stream.id))];
    return sort === 'mcap' ? rankByMarketCap(combined) : rankByViewers(combined);
  }, [directory.entries, more.entries, sort]);
  const visible = ranked.slice(0, shown);
  const serverHasMore = start === null ? cursor !== null : more.hasMore;

  const showMore = () => {
    if (shown >= ranked.length) {
      if (start === null && cursor !== null) setStart(cursor);
      else more.loadMore();
    }
    setShown(current => current + liveAllPageSize);
  };

  const changeSort = (next: LiveSort) => {
    setShown(liveAllPageSize);
    router.replace(`${pathname}?sort=${next}`, { scroll: false });
  };

  return (
    <div className="mx-auto flex w-full max-w-[1920px] flex-col gap-6 px-4 py-6 md:px-6">
      <header className="flex flex-col gap-3 md:flex-row md:items-end md:justify-between">
        <div className="flex flex-col gap-1">
          <Link href="/live" className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-text-muted hover:text-text">
            <ChevronLeft aria-hidden="true" className="size-4" />
            Live
          </Link>
          <h1 className="text-xl font-bold text-text">{liveSortLabels[sort]}</h1>
          <p className="text-sm text-text-muted">Live now, sorted by {sort === 'mcap' ? 'market cap' : 'viewers'}</p>
        </div>
        <SegmentedControl label="Sort by" options={sortOptions} value={sort} onChange={changeSort} />
      </header>

      {directory.state.state === 'loading' ? (
        <ol aria-busy="true" className={streamGridClass}>
          {Array.from({ length: 8 }, (_, index) => <li key={index}><StreamCardSkeleton /></li>)}
        </ol>
      ) : failure ? (
        <div className="rounded-lg bg-surface-1">
          <LiveUnavailable variant="panel" operation={failure.operation} status={failure.status} message={failure.message} onRetry={directory.refresh} />
        </div>
      ) : ranked.length === 0 ? (
        <div className="rounded-lg bg-surface-1"><NobodyLive /></div>
      ) : (
        <>
          <DirectoryNotices directory={directory} />
          <ol className={streamGridClass} aria-label={`${liveSortLabels[sort]}, ${ranked.length} live`}>
            {visible.map(entry => (
              <li key={entry.stream.id} className="min-w-0">
                <StreamCard entry={entry} creatorLabel={directory.creatorLabel(entry.creatorWallet)} />
              </li>
            ))}
          </ol>
          {more.error ? <p role="alert" className="text-sm text-danger">Couldn't load more streams, try again</p> : null}
          <LoadMore
            auto={false}
            hasMore={shown < ranked.length || serverHasMore}
            loading={more.loading}
            error={more.error !== null}
            onLoadMore={showMore}
            label="Show more"
            endLabel="No more live streams"
          />
        </>
      )}
    </div>
  );
}

export default function LiveAllModule() {
  return (
    <Suspense fallback={<ol aria-busy="true" className={`${streamGridClass} px-4 py-6 md:px-6`}>{Array.from({ length: 8 }, (_, index) => <li key={index}><StreamCardSkeleton /></li>)}</ol>}>
      <LiveAllView />
    </Suspense>
  );
}
