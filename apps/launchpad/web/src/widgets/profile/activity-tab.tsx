'use client';

import { useEffect, useId, useMemo, useState, type ReactNode } from 'react';
import { History, Search } from 'lucide-react';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { CollectedPlaceholder, useProfileIssue } from './profile-issues';
import { Button, EmptyState, ErrorState, LoadMore, NoticeBar, SelectPill, Skeleton, UnavailableState, type SelectOption } from '@/ui/primitives';
import { actionLabel } from './action-badge';
import { ActivityRow, type ActivityTokenIdentity } from './activity-row';
import { activityKinds, filterActivity, tradesUnavailableNotice, useActivityLedger, type ActivityEvent, type ActivityFilter } from './activity-transport';

export interface ActivityTabProps {
  wallet: string;
  /** The signed-in owner gets a call to action on the empty ledger; visitors get neutral copy. */
  isOwner?: boolean;
}

const filterOptions: SelectOption<ActivityFilter>[] = [
  { value: 'all', label: 'All actions' },
  ...activityKinds.map(kind => ({ value: kind, label: actionLabel(kind) })),
];

function useNow(intervalMs = 60_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), intervalMs);
    return () => window.clearInterval(timer);
  }, [intervalMs]);
  return now;
}

/**
 * Activity ledger: Paxscan ERC-20 transfers (Send, Receive), newest first,
 * with action filter, search over the loaded rows and explicit load more.
 * Buys and sells are not listed because no production service publishes a
 * per-wallet trade history; the notice above the ledger says so.
 */
export function ActivityTab({ wallet, isOwner = false }: ActivityTabProps) {
  const activity = useActivityLedger(wallet);
  const collected = useProfileIssue('activity', activity.state === 'error' ? { label: 'Activity', error: activity.error, retry: activity.retry } : null);
  const [filter, setFilter] = useState<ActivityFilter>('all');
  const [search, setSearch] = useState('');
  const now = useNow();
  const searchId = useId();
  const { events, hasMore } = activity.ledger;

  const tokens = useMemo(() => [...new Set(events.map(event => event.tokenAddress))], [events]);
  const metadata = useTokenMetadataBatch(tokens);

  const explorerIdentity = useMemo(() => {
    const map = new Map<string, Pick<ActivityTokenIdentity, 'name' | 'symbol' | 'logo' | 'decimals'>>();
    for (const event of events) {
      const known = map.get(event.tokenAddress);
      map.set(event.tokenAddress, {
        name: known?.name ?? event.tokenName,
        symbol: known?.symbol ?? event.tokenSymbol,
        logo: known?.logo ?? event.tokenIcon,
        decimals: known?.decimals ?? event.decimals,
      });
    }
    return map;
  }, [events]);

  const identityOf = useMemo(() => (event: ActivityEvent): ActivityTokenIdentity => {
    const meta = metadata.data?.[event.tokenAddress] ?? null;
    const explorer = explorerIdentity.get(event.tokenAddress);
    const pool = meta?.pool_address && /^0x[0-9a-fA-F]{40}$/.test(meta.pool_address) ? meta.pool_address : null;
    return {
      name: meta?.name?.trim() || explorer?.name || null,
      symbol: meta?.symbol?.trim() || explorer?.symbol || null,
      logo: meta?.images?.logo ?? explorer?.logo ?? null,
      decimals: typeof meta?.decimals === 'number' ? meta.decimals : explorer?.decimals ?? event.decimals,
      poolAddress: event.poolAddress ?? pool,
    };
  }, [metadata.data, explorerIdentity]);

  const visible = useMemo(() => filterActivity(events, filter, search, identityOf), [events, filter, search, identityOf]);
  const narrowed = filter !== 'all' || search.trim() !== '';

  const toolbar = (
    <div className="flex flex-wrap items-center gap-2">
      <div className="flex h-control-height-md min-w-0 flex-1 basis-56 items-center gap-2 rounded-sm bg-surface-2 px-3 focus-within:outline-2 focus-within:outline-focus">
        <Search aria-hidden="true" className="size-icon-md shrink-0 text-text-faint" />
        <label htmlFor={searchId} className="sr-only">Search activity</label>
        <input
          id={searchId}
          type="search"
          value={search}
          onChange={event => setSearch(event.target.value)}
          placeholder="Search activity..."
          autoComplete="off"
          spellCheck={false}
          className="min-w-0 flex-1 bg-transparent text-sm text-text outline-none placeholder:text-text-faint"
        />
      </div>
      <SelectPill options={filterOptions} value={filter} onChange={setFilter} label="Filter by action" size="md" />
    </div>
  );

  let body: ReactNode;
  if (activity.state === 'loading') {
    body = <Skeleton variant="rows" rows={5} label="activity" />;
  } else if (activity.state === 'unsupported' && activity.unavailable) {
    body = <UnavailableState title="Activity" operation={activity.unavailable.operation} status={activity.unavailable.status} message="Check back in a bit" onRetry={activity.retry} />;
  } else if (activity.state === 'error' && collected) {
    body = <CollectedPlaceholder label="Transfers" />;
  } else if (activity.state === 'error') {
    body = <ErrorState title="Can't load activity right now" message="Check your connection and try again" error={activity.error ?? undefined} onRetry={activity.retry} />;
  } else if (events.length === 0 && !hasMore) {
    body = (
      <EmptyState
        icon={History}
        title="No activity yet"
        body={isOwner ? 'Your transfers show up here' : "This wallet hasn't moved any coins yet"}
        action={isOwner ? <Button href="/explore" variant="secondary" size="sm">Browse coins</Button> : undefined}
      />
    );
  } else {
    body = (
      <>
        <div className="overflow-x-auto">
          <table className="w-full table-fixed text-left sm:table-auto">
            <caption className="sr-only">Wallet activity, newest first</caption>
            <thead>
              <tr className="h-table-header-height text-xs font-semibold text-text-faint">
                <th scope="col" className="pl-3 pr-2 font-semibold">Coin</th>
                <th scope="col" className="w-28 px-2 font-semibold">Action</th>
                <th scope="col" className="px-2 text-right font-semibold">Amount</th>
                <th scope="col" className="hidden px-2 text-right font-semibold sm:table-cell">Time</th>
                <th scope="col" className="w-10 pl-1 pr-2"><span className="sr-only">Explorer link</span></th>
              </tr>
            </thead>
            <tbody>
              {visible.map(event => <ActivityRow key={event.id} event={event} identity={identityOf(event)} now={now} />)}
              {visible.length === 0 ? (
                <EmptyState
                  variant="table"
                  colSpan={5}
                  announce
                  title={narrowed ? 'No activity matches' : 'Nothing to show yet'}
                  body={narrowed ? (hasMore ? 'Load more to search older activity' : 'Try a different filter or search') : 'Loading older activity...'}
                />
              ) : null}
            </tbody>
          </table>
        </div>
        {metadata.isError ? <p className="px-3 text-xs text-text-faint">Some coin names can't load right now.</p> : null}
        <LoadMore onLoadMore={activity.loadMore} hasMore={hasMore} loading={activity.loadingMore} error={activity.moreError} auto={false} label="Load more activity" endLabel="That's everything" />
      </>
    );
  }

  return (
    <section aria-label="Activity" className="flex flex-col gap-3">
      {toolbar}
      <NoticeBar tone="neutral" wrap>{tradesUnavailableNotice}</NoticeBar>
      <div className="rounded-lg bg-surface-1 p-1">{body}</div>
    </section>
  );
}
