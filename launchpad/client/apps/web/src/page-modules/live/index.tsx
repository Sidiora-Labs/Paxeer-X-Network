'use client';

import { Radio } from 'lucide-react';
import { Button, EmptyState, NoticeBar, StateNotice } from '@/ui/primitives';
import type { Loadable } from '@/core/market/unsupported';
import { HeroCarousel, HeroCarouselSkeleton, HeroFramePlaceholder } from '@/widgets/live/hero-carousel';
import { LiveUnavailable } from '@/widgets/live/live-unavailable';
import { RankedRow } from '@/widgets/live/ranked-row';
import { useLiveDirectory, type LiveDirectory, type LiveDirectoryData, type SourceState } from '@/widgets/live/use-live-directory';

export interface DirectoryFailure { operation: string; status: number | null; message: string }

/** The real failure of the directory request, for the unavailable panel; null while loading or ready. */
export function directoryFailure(state: Loadable<LiveDirectoryData>): DirectoryFailure | null {
  if (state.state === 'unsupported') return { operation: state.operation, status: state.status, message: state.message };
  if (state.state === 'error') {
    const status = (state.error as { status?: unknown }).status;
    return { operation: 'IF_STREAM_LIST', status: typeof status === 'number' ? status : null, message: state.error.message };
  }
  return null;
}

/** Enrichment sources that failed during the last refresh, named in plain words (no operation ids or codes). */
export function degradedSources(data: LiveDirectoryData): string[] {
  const out: string[] = [];
  const describe = (label: string, source: SourceState) => {
    if (source.state === 'unsupported' || source.state === 'error') out.push(label);
  };
  describe('Market caps', data.sources.stats);
  describe('Coin details', data.sources.identity);
  describe('Coin names and images', data.sources.metadata);
  return out;
}

/** The page names itself for assistive tech only: like the reference, the hero starts directly under the ticker. */
export function LiveHeading() {
  return <h1 className="sr-only">Live streams</h1>;
}

export function DirectoryNotices({ directory }: { directory: LiveDirectory }) {
  if (directory.state.state !== 'ready') return null;
  const degraded = degradedSources(directory.state.data);
  const truncated = directory.state.data.truncated;
  if (degraded.length === 0 && !truncated) return null;
  return (
    <div className="flex flex-col gap-2">
      {degraded.length > 0 ? (
        <NoticeBar tone="warning" title="Some details can't load right now">
          Missing for now: {degraded.join(', ').toLowerCase()}
        </NoticeBar>
      ) : null}
      {truncated ? (
        <NoticeBar tone="info">Showing the top {directory.entries.length} live streams</NoticeBar>
      ) : null}
    </div>
  );
}

export function NobodyLive() {
  return (
    <EmptyState
      icon={Radio}
      title="No live streams right now"
      body="Streams show up here as soon as a creator goes live from their coin page"
      action={<Button href="/create" size="sm">Create coin</Button>}
      secondaryAction={<Button href="/explore" variant="secondary" size="sm" className="bg-surface-3">Browse coins</Button>}
      headingLevel={2}
      announce
      className="p-4 sm:p-8"
    />
  );
}

export default function LiveModule() {
  const directory = useLiveDirectory();
  const { state } = directory;
  const failure = directoryFailure(state);
  const ready = state.state === 'ready' && directory.entries.length > 0;

  return (
    <div className="mx-auto flex w-full max-w-[1920px] flex-col gap-10 px-4 pb-8 pt-3 md:px-6">
      <LiveHeading />
      {state.state === 'loading' ? (
        <div aria-busy="true" className="flex flex-col gap-10">
          <span className="sr-only" role="status">Loading streams</span>
          <HeroCarouselSkeleton />
          <RankedRow sort="viewers" entries={[]} creatorLabel={directory.creatorLabel} loading />
          <RankedRow sort="mcap" entries={[]} creatorLabel={directory.creatorLabel} loading />
        </div>
      ) : ready ? (
        <>
          <DirectoryNotices directory={directory} />
          <HeroCarousel entries={directory.featured} creatorLabel={directory.creatorLabel} />
          <RankedRow sort="viewers" entries={directory.byViewers} creatorLabel={directory.creatorLabel} />
          <RankedRow sort="mcap" entries={directory.byMcap} creatorLabel={directory.creatorLabel} />
        </>
      ) : (
        // Unavailable or nobody live: the hero frame and both row frames stay, each carrying a compact notice.
        <>
          <HeroFramePlaceholder>
            {failure ? (
              <LiveUnavailable operation={failure.operation} status={failure.status} message={failure.message} onRetry={directory.refresh} surface={2} />
            ) : (
              <NobodyLive />
            )}
          </HeroFramePlaceholder>
          {(['viewers', 'mcap'] as const).map(sort => (
            <RankedRow
              key={sort}
              sort={sort}
              entries={[]}
              creatorLabel={directory.creatorLabel}
              placeholder
              notice={failure
                ? <LiveUnavailable operation={failure.operation} status={failure.status} surface={2} />
                : <StateNotice kind="empty" surface={2}>No live streams right now</StateNotice>}
            />
          ))}
        </>
      )}
    </div>
  );
}
