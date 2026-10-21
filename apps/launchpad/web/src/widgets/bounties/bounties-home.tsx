'use client';
import { useCallback, useMemo } from 'react';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { useAccount } from 'wagmi';
import { BadgeCheck, ClipboardCheck, Target, Wallet } from 'lucide-react';
import { Button, EmptyState, ErrorState, LoadMore, UnavailableState } from '@/ui/primitives';
import { useAuthModal } from '@/shell/auth-modal-store';
import { statsByPool, useStatsBatch } from '@/core/market/use-stats-batch';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { marketLimits } from '@/page-modules/token/market-session';
import { bountiesContract, type Bounty } from './bounties-contract';
import { BountiesHeader, CreateBountyButton, CreateBountyFab, useCreateBountyGate } from './bounties-header';
import { BountiesStats } from './bounties-stats';
import { BountiesTabs, bountiesTabPrefix, bountyTabCopy, bountyTabCounts, isBountyTab, type BountyTab } from './bounties-tabs';
import { BountyCard, BountyCardSkeleton } from './bounty-card';
import { BountyHighlights } from './highest-value-rail';
import { useBounties, useBountiesAvailability, useBountiesStats } from './use-bounties';
import { useSessionWallet } from './use-bounty-mutations';
import { useGatewaySessionOperations } from '@/core/market/operations';

const emptyIcon: Record<BountyTab, typeof Target> = { open: Target, reviewing: ClipboardCheck, paid: BadgeCheck, mine: Target };
const emptyBody: Record<BountyTab, string> = {
  open: 'Post a task with a USDL reward and let the community deliver',
  reviewing: 'Bounties show up here once submissions close and the creator picks winners',
  paid: 'Paid bounties and their winners show up here',
  mine: 'Bounties you create show up here',
};

const gridClass = 'grid grid-cols-1 gap-4 min-[900px]:grid-cols-2 xl:grid-cols-3';

function SkeletonGrid({ count = 6 }: { count?: number }) {
  return (
    <div className={gridClass} aria-busy="true">
      <span className="sr-only">Loading bounties</span>
      {Array.from({ length: count }).map((_, index) => <BountyCardSkeleton key={index} />)}
    </div>
  );
}

/** Route-level fallback while the search params boundary resolves. */
export function BountiesHomeSkeleton() {
  return (
    <div className="mx-auto flex w-full max-w-content-narrow-max-width flex-col gap-6 px-4 py-6">
      <SkeletonGrid />
    </div>
  );
}

function BountyGrid({ items, now }: { items: Bounty[]; now?: number }) {
  const pools = useMemo(() => [...new Set(items.flatMap(item => (item.poolAddress ? [item.poolAddress.toLowerCase()] : [])))].slice(0, marketLimits.rankingPage), [items]);
  const batch = useStatsBatch(pools);
  const stats = useMemo(() => statsByPool(batch.data), [batch.data]);
  const identities = useWalletIdentities(items.map(item => item.creatorWallet));
  return (
    <div className={gridClass}>
      {items.map(item => {
        const identity = identities.identities[item.creatorWallet.toLowerCase()];
        return (
          <BountyCard
            key={item.bountyId}
            bounty={item}
            creator={{ label: identities.label(item.creatorWallet), avatarUrl: identity?.avatarUrl ?? null }}
            stats={item.poolAddress ? stats.get(item.poolAddress.toLowerCase()) ?? null : null}
            now={now}
          />
        );
      })}
    </div>
  );
}

export default function BountiesHome() {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const requested = searchParams.get('tab');
  const tab: BountyTab = isBountyTab(requested) ? requested : 'open';
  const { address } = useAccount();
  const { openModal } = useAuthModal();

  const availability = useBountiesAvailability();
  const serviceUp = availability.loadable.state === 'ready';
  const openList = useBounties({ state: 'open' }, { enabled: serviceUp });
  const activeList = useBounties(tab === 'mine' ? {} : { state: tab }, { enabled: serviceUp });
  const serviceDown = availability.loadable.state === 'unsupported' || availability.loadable.state === 'error';
  const paidList = useBounties({ state: 'paid' }, { enabled: serviceUp });
  const stats = useBountiesStats(serviceUp);
  const sessionOperations = useGatewaySessionOperations();
  const sessionWallet = useSessionWallet();
  const gate = useCreateBountyGate(availability.loadable, sessionOperations !== null && sessionWallet !== null);

  const items = useMemo(() => (tab === 'mine' ? bountiesContract.mine(activeList.items, address) : activeList.items), [tab, activeList.items, address]);
  const statsOpenCount = stats.state === 'ready' ? stats.data.data.openCount : null;
  const counts = bountyTabCounts({ statsOpenCount, open: openList, paid: paidList, active: { tab, complete: activeList.complete, count: items.length } });

  const selectTab = useCallback((next: BountyTab) => {
    if (next === 'mine' && !address) { openModal('Connect wallet to see your bounties'); return; }
    router.replace(next === 'open' ? pathname : `${pathname}?tab=${next}`, { scroll: false });
  }, [address, openModal, pathname, router]);

  const panelIds = { panel: `${bountiesTabPrefix}-panel-${tab}`, tab: `${bountiesTabPrefix}-tab-${tab}` };
  const copy = bountyTabCopy[tab];

  let content: React.ReactNode;
  if (availability.loadable.state === 'loading') content = <SkeletonGrid />;
  else if (availability.loadable.state === 'unsupported') {
    content = <div className="rounded-lg bg-surface-1"><UnavailableState title="Bounties" plural operation={availability.loadable.operation} status={availability.loadable.status} message="Check back soon" onRetry={availability.retry} /></div>;
  } else if (availability.loadable.state === 'error') {
    content = <div className="rounded-lg bg-surface-1"><ErrorState title="Bounties can't load right now" message="Something went wrong, try again" onRetry={availability.retry} /></div>;
  } else if (tab === 'mine' && !address) {
    content = <div className="rounded-lg bg-surface-1"><EmptyState icon={Wallet} title="Connect wallet to see your bounties" body="Bounties you create show up here" action={<Button variant="primary" size="md" onClick={() => openModal('Connect wallet to see your bounties')}>Connect wallet</Button>} /></div>;
  } else if (activeList.loadable.state === 'loading') content = <SkeletonGrid />;
  else if (activeList.loadable.state === 'unsupported') {
    content = <div className="rounded-lg bg-surface-1"><UnavailableState title="Bounties" plural operation={activeList.loadable.operation} status={activeList.loadable.status} onRetry={activeList.refetch} /></div>;
  } else if (activeList.loadable.state === 'error') {
    content = <div className="rounded-lg bg-surface-1"><ErrorState title="Bounties can't load right now" message="Something went wrong, try again" onRetry={activeList.loadable.retry} /></div>;
  } else if (items.length === 0 && !activeList.hasMore) {
    content = (
      <div className="rounded-lg bg-surface-1">
        <EmptyState
          icon={emptyIcon[tab]}
          title={copy.empty}
          body={emptyBody[tab]}
          action={tab === 'open' || tab === 'mine' ? <CreateBountyButton gate={gate} label="Create bounty" /> : undefined}
          announce
        />
      </div>
    );
  } else {
    content = (
      <div className="flex flex-col gap-4">
        <BountyGrid items={items} />
        <LoadMore onLoadMore={activeList.loadMore} hasMore={activeList.hasMore} loading={activeList.loadingMore} auto={false} endLabel="No more bounties" />
      </div>
    );
  }

  const statsProps = { stats, openCount: openList.complete ? openList.items.length : null, openLoading: serviceUp && openList.loadable.state === 'loading', serviceDown };

  return (
    <div className="mx-auto flex w-full max-w-content-narrow-max-width flex-col gap-4 px-4 pb-24 pt-5 md:gap-5 md:pb-6">
      <BountiesHeader
        tab={tab}
        gate={gate}
        stats={<BountiesStats {...statsProps} variant="chips" />}
        inlineStats={<BountiesStats {...statsProps} variant="inline" />}
      />
      {/*
        Phones: tabs, the swipeable highlight cards, then the list. md to xl:
        the list comes first and the highlight cards stack full width below it.
        From xl: the highlights sit in the right rail beside the tabs and list.
      */}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1fr)_var(--spacing-right-rail-width)] xl:gap-x-6">
        <BountiesTabs active={tab} onChange={selectTab} counts={counts} mineLocked={!address} className="min-w-0 xl:col-start-1 xl:row-start-1" />
        <div role="tabpanel" id={panelIds.panel} aria-labelledby={panelIds.tab} tabIndex={0} data-region="bounty-list" className="order-2 flex min-w-0 flex-col gap-4 md:order-none xl:col-start-1 xl:row-start-2">
          {content}
        </div>
        <aside aria-label="Bounty highlights" data-region="bounty-highlights" className="order-1 min-w-0 md:order-none xl:sticky xl:top-4 xl:col-start-2 xl:row-span-2 xl:row-start-1 xl:self-start">
          <BountyHighlights enabled={serviceUp} serviceDown={serviceDown} />
        </aside>
      </div>
      <CreateBountyFab gate={gate} />
    </div>
  );
}
