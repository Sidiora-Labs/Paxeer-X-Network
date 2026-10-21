'use client';
import { UnderlineTabs, type TabItem } from '@/ui/primitives';
import type { BountyListState } from './bounties-contract';

export type BountyTab = BountyListState | 'mine';
export const bountyTabs: readonly BountyTab[] = ['open', 'reviewing', 'paid', 'mine'];
export const bountiesTabPrefix = 'bounties';

export interface BountyTabCopy { label: string; title: string; subtitle: string; empty: string }

export const bountyTabCopy: Record<BountyTab, BountyTabCopy> = {
  open: { label: 'Bounties', title: 'Open bounties', subtitle: 'Tasks taking submissions right now', empty: 'No open bounties yet' },
  reviewing: { label: 'Reviewing', title: 'Under review', subtitle: 'Submissions are closed, the creator is picking winners', empty: 'Nothing under review right now' },
  paid: { label: 'Paid', title: 'Paid out', subtitle: 'Bounties that paid their winners', empty: 'No bounties paid out yet' },
  mine: { label: 'Mine', title: 'Your bounties', subtitle: 'Bounties you created', empty: "You haven't created a bounty yet" },
};

export function isBountyTab(value: string | null | undefined): value is BountyTab {
  return typeof value === 'string' && (bountyTabs as readonly string[]).includes(value);
}

export interface CompleteList { complete: boolean; items: readonly unknown[] }

/**
 * Tab counts from real totals only: Bounties uses the IF_BOUNTIES_STATS open
 * count when served, else a complete open list; Paid and the active tab use a
 * complete list (every page loaded, no further cursor). Anything else shows no
 * badge rather than a partial number.
 */
export function bountyTabCounts(input: { statsOpenCount: number | null; open: CompleteList; paid: CompleteList; active: { tab: BountyTab; complete: boolean; count: number } }): Partial<Record<BountyTab, number>> {
  const result: Partial<Record<BountyTab, number>> = {};
  if (input.active.complete) result[input.active.tab] = input.active.count;
  if (input.paid.complete) result.paid = input.paid.items.length;
  if (input.statsOpenCount !== null) result.open = input.statsOpenCount;
  else if (input.open.complete) result.open = input.open.items.length;
  return result;
}

export interface BountiesTabsProps {
  active: BountyTab;
  onChange(tab: BountyTab): void;
  /** Only real totals: a count is passed when the page is complete and has no further cursor. */
  counts?: Partial<Record<BountyTab, number>>;
  /** No wallet connected: Mine shows the lock and the caller opens sign-in. */
  mineLocked?: boolean;
  className?: string;
}

export function BountiesTabs({ active, onChange, counts = {}, mineLocked = false, className }: BountiesTabsProps) {
  const tabs: TabItem<BountyTab>[] = bountyTabs.map(id => ({ id, label: bountyTabCopy[id].label, count: counts[id], locked: id === 'mine' && mineLocked }));
  return <UnderlineTabs tabs={tabs} active={active} onChange={onChange} label="Bounty lists" idPrefix={bountiesTabPrefix} className={className} />;
}
