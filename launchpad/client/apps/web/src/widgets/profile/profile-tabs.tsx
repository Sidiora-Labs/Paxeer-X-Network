'use client';

import { useCallback, type ReactNode } from 'react';
import { usePathname, useRouter, useSearchParams } from 'next/navigation';
import { DropdownMenu, TabStrip } from '@/ui/primitives';

export type ProfileTabKey = 'open' | 'closed' | 'replies' | 'activity' | 'rewards' | 'referrals' | 'watchlist' | 'accounting' | 'settings';

export const PRIMARY_TABS: readonly ProfileTabKey[] = ['open', 'closed', 'replies', 'activity'];
/** Owner-only sections that live behind the More caret. */
export const MORE_TABS: readonly ProfileTabKey[] = ['rewards', 'referrals', 'watchlist', 'accounting', 'settings'];

export const profileTabLabel: Record<ProfileTabKey, string> = {
  open: 'Open',
  closed: 'Closed',
  replies: 'Replies',
  activity: 'Activity',
  rewards: 'Rewards',
  referrals: 'Referrals',
  watchlist: 'Watchlist',
  accounting: 'PnL',
  settings: 'Settings',
};

export const TAB_PARAM = 'tab';

function isTabKey(value: string): value is ProfileTabKey {
  return (PRIMARY_TABS as readonly string[]).includes(value) || (MORE_TABS as readonly string[]).includes(value);
}

/** `?tab=` value to a tab key; unknown values and owner-only tabs for visitors fall back to Open. */
export function parseProfileTab(value: string | null | undefined, isOwner: boolean): ProfileTabKey {
  if (!value || !isTabKey(value)) return 'open';
  if (!isOwner && (MORE_TABS as readonly string[]).includes(value)) return 'open';
  return value;
}

/** Rewrites only the tab parameter, keeping every other query parameter. Open (the default) drops the parameter. */
export function profileTabHref(pathname: string, search: string, tab: ProfileTabKey): string {
  const params = new URLSearchParams(search);
  if (tab === 'open') params.delete(TAB_PARAM);
  else params.set(TAB_PARAM, tab);
  const query = params.toString();
  return query ? `${pathname}?${query}` : pathname;
}

export interface ProfileTabsProps {
  isOwner: boolean;
  /** Content per tab; a missing panel renders nothing for that tab. */
  panels: Partial<Record<ProfileTabKey, ReactNode>>;
  idPrefix?: string;
  className?: string;
}

export default function ProfileTabs({ isOwner, panels, idPrefix = 'profile-tabs', className }: ProfileTabsProps) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const active = parseProfileTab(searchParams.get(TAB_PARAM), isOwner);
  const search = searchParams.toString();

  const select = useCallback((tab: ProfileTabKey) => {
    router.replace(profileTabHref(pathname, search, tab), { scroll: false });
  }, [router, pathname, search]);

  const inMore = (MORE_TABS as readonly string[]).includes(active);
  const panelId = `${idPrefix}-panel-${active}`;

  return (
    <section className={className}>
      <div className="px-2 md:px-3">
        <TabStrip
          label="Profile sections"
          idPrefix={idPrefix}
          tabs={PRIMARY_TABS.map(id => ({ id, label: profileTabLabel[id] }))}
          active={active}
          onChange={select}
          trailing={isOwner ? (
            <DropdownMenu
              label={inMore ? profileTabLabel[active] : 'More'}
              menuLabel="More profile sections"
              showLabel
              align="end"
              size="sm"
              triggerClassName={inMore ? 'text-text' : undefined}
              items={MORE_TABS.map(id => ({ id, label: profileTabLabel[id], selected: active === id, onSelect: () => select(id) }))}
            />
          ) : null}
        />
      </div>
      <div
        role="tabpanel"
        id={panelId}
        aria-labelledby={inMore ? undefined : `${idPrefix}-tab-${active}`}
        aria-label={inMore ? profileTabLabel[active] : undefined}
        tabIndex={0}
        className="px-4 py-3 md:px-5"
      >
        {panels[active] ?? null}
      </div>
    </section>
  );
}
