'use client';

import { useCallback, useSyncExternalStore } from 'react';
import Link from 'next/link';
import Image from 'next/image';
import { usePathname } from 'next/navigation';
import { useAccount } from 'wagmi';
import { ChevronsRight, PanelLeftClose, PanelLeftOpen } from 'lucide-react';
import type { V2ContractTypes } from '@kindle/sdk';
import { isRouteActive, routeForPath } from '@/core/app-foundation/navigation';
import { pnlApiUrl } from '@/core/sdk-config';
import { accountRequest } from '@/widgets/profile/account-transport';
import { usePrivateAccountQuery } from '@/widgets/profile/use-private-account-query';
import { Tooltip } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { createItem, navItems, type NavItem } from './nav-config';

const RAIL_KEY = 'sidiora-rail-expanded';
const railListeners = new Set<() => void>();

function readRail(): boolean {
  try { return localStorage.getItem(RAIL_KEY) === 'true'; } catch { return false; }
}

function writeRail(value: boolean) {
  try { localStorage.setItem(RAIL_KEY, String(value)); } catch {}
  for (const listener of railListeners) listener();
}

function subscribeRail(listener: () => void) {
  railListeners.add(listener);
  const onStorage = (event: StorageEvent) => { if (event.key === RAIL_KEY) listener(); };
  window.addEventListener('storage', onStorage);
  return () => { railListeners.delete(listener); window.removeEventListener('storage', onStorage); };
}

const collapsed = () => false;

/** Icon rail expansion, collapsed by default, persisted per device. */
export function useRailState(): { expanded: boolean; toggle(): void; setExpanded(value: boolean): void } {
  const expanded = useSyncExternalStore(subscribeRail, readRail, collapsed);
  const toggle = useCallback(() => writeRail(!readRail()), []);
  const setExpanded = useCallback((value: boolean) => writeRail(value), []);
  return { expanded, toggle, setExpanded };
}

type RewardsResponse = V2ContractTypes['RewardsResponse'];

function readRewards() {
  return accountRequest<RewardsResponse>(pnlApiUrl('/v2/me/rewards?limit=50'));
}

/** True when the account's reward entitlements include a nonzero claimable amount (IF_REWARDS, first page). */
function useRewardsClaimable(): { claimable: boolean; sessionReady: boolean } {
  const rewards = usePrivateAccountQuery('rewards', readRewards, ['']);
  const claimable = rewards.data?.data.items.some(row => /^[1-9][0-9]*$/.test(row.claimableRaw)) ?? false;
  return { claimable, sessionReady: !!rewards.session };
}

function RailItem({ item, active, expanded, dot = false }: { item: NavItem; active: boolean; expanded: boolean; dot?: boolean }) {
  const Icon = item.icon;
  const link = (
    <Link
      href={item.href}
      aria-current={active ? 'page' : undefined}
      aria-label={expanded ? undefined : item.label}
      data-route-id={routeForPath(item.href)?.id}
      className={cn(
        'relative flex h-10 items-center rounded-sm text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text active:bg-surface-active',
        expanded ? 'w-full gap-3 px-3' : 'w-10 justify-center',
        active && 'bg-surface-3 text-text',
      )}
    >
      <Icon aria-hidden="true" className="size-icon-rail shrink-0" />
      {expanded ? <span className="truncate text-sm font-bold">{item.label}</span> : null}
      {dot ? (
        <>
          <span aria-hidden="true" className={cn('absolute size-2 rounded-pill bg-accent', expanded ? 'right-3 top-1/2 -translate-y-1/2' : 'right-1.5 top-1.5')} />
          <span className="sr-only">rewards available</span>
        </>
      ) : null}
    </Link>
  );
  return <li className={cn(!expanded && 'flex justify-center')}>{expanded ? link : <Tooltip content={item.label} align="start">{link}</Tooltip>}</li>;
}

/** The most specific nav href matching the path, so /docs/terms lights "Terms" and not also "Help". */
export function activeNavHref(pathname: string, hrefs: readonly string[]): string | null {
  let best: string | null = null;
  for (const href of hrefs) if (isRouteActive(pathname, href) && (best === null || href.length > best.length)) best = href;
  return best;
}

const listClass = (expanded: boolean) => cn('flex flex-col gap-1', !expanded && 'items-center');

const utilityButton = 'flex h-10 items-center rounded-sm text-text-muted transition-colors duration-fast hover:bg-surface-hover hover:text-text active:bg-surface-active';

/**
 * Icon rail, shown from md (768) up; below md the bottom tab bar
 * (mobile-bottom-nav.tsx) is the navigation. Anatomy follows the reference:
 * logo with the expand chevron beside it, primary destinations, a spacing gap,
 * secondary destinations, create, then a utility cluster pinned to the bottom
 * (help, legal, sidebar toggle). Groups are separated by spacing only.
 */
export default function SidebarNav() {
  const pathname = usePathname();
  const { expanded, toggle, setExpanded } = useRailState();
  const { address, isConnected } = useAccount();
  const { claimable, sessionReady } = useRewardsClaimable();
  const walletReady = isConnected && !!address;

  const visible = (item: NavItem) => item.requires === 'public' || (item.requires === 'wallet' && walletReady) || (item.requires === 'session' && sessionReady);
  const main = navItems.filter(item => item.section === 'main' && visible(item));
  const primary = main.filter(item => item.group !== 'secondary');
  const secondary = main.filter(item => item.group === 'secondary');
  const signedIn = navItems.filter(item => item.section === 'signed-in' && visible(item));
  const bottom = navItems.filter(item => item.section === 'bottom' && visible(item));
  const activeHref = activeNavHref(pathname, [...main, ...signedIn, ...bottom, createItem].map(item => item.href));
  const CreateIcon = createItem.icon;
  const PanelIcon = expanded ? PanelLeftClose : PanelLeftOpen;
  const item = (entry: NavItem, dot = false) => <RailItem key={entry.id} item={entry} active={entry.href === activeHref} expanded={expanded} dot={dot} />;

  const createLink = (
    <Link
      href={createItem.href}
      aria-label={expanded ? undefined : 'Create coin'}
      aria-current={activeHref === createItem.href ? 'page' : undefined}
      data-route-id={routeForPath(createItem.href)?.id}
      className={cn(
        'flex h-9 items-center rounded-sm bg-accent text-on-accent transition-colors duration-fast hover:bg-accent-hover active:bg-accent-active',
        expanded ? 'w-full gap-3 px-3' : 'w-9 justify-center',
      )}
    >
      <CreateIcon aria-hidden="true" className="size-icon-rail shrink-0" />
      {expanded ? <span className="text-sm font-bold">Create coin</span> : null}
    </Link>
  );

  const panelToggle = (
    <button
      type="button"
      onClick={toggle}
      aria-expanded={expanded}
      aria-label="Toggle sidebar"
      className={cn(utilityButton, expanded ? 'w-full gap-3 px-3' : 'w-10 justify-center')}
    >
      <PanelIcon aria-hidden="true" className="size-icon-rail shrink-0" />
      {expanded ? <span className="truncate text-sm font-bold">Collapse sidebar</span> : null}
    </button>
  );

  return (
    <>
      {expanded ? (
        <button
          type="button"
          aria-label="Collapse navigation"
          onClick={() => setExpanded(false)}
          className="fixed inset-0 z-[calc(var(--sl-z-rail)-1)] hidden bg-[var(--sl-tint-scrim)] md:block xl:hidden"
        />
      ) : null}
      <aside
        aria-label="Navigation rail"
        className={cn(
          'fixed inset-y-0 left-0 z-[var(--sl-z-rail)] hidden flex-col bg-surface-1 transition-[width] duration-base md:flex',
          expanded ? 'w-rail-expanded-width' : 'w-rail-width',
        )}
      >
        <div className={cn('flex h-header-height shrink-0 items-center', expanded ? 'px-3' : 'justify-center pl-1')}>
          <Link href="/" aria-label="Sidiora Launch home" className="flex items-center gap-2 rounded-sm p-0.5">
            <Image src="/Kindle-Launch-logo-dark.png" alt="" width={28} height={28} className="size-7" />
            {expanded ? <span className="text-base font-extrabold text-text">Sidiora</span> : null}
          </Link>
          {expanded ? null : (
            <button
              type="button"
              onClick={toggle}
              aria-expanded={false}
              aria-label="Expand navigation"
              title="Expand navigation"
              className="flex h-7 w-4 items-center justify-center rounded-xs text-text-faint transition-colors duration-fast hover:text-text"
            >
              <ChevronsRight aria-hidden="true" className="size-3.5" />
            </button>
          )}
        </div>

        <nav aria-label="Primary" className={cn('flex min-h-0 flex-1 flex-col overflow-y-auto pb-2', expanded ? 'px-3' : 'items-center px-2')}>
          <ul data-rail-group="primary" className={listClass(expanded)}>{primary.map(entry => item(entry))}</ul>
          {secondary.length > 0 ? <ul data-rail-group="secondary" aria-label="More destinations" className={cn(listClass(expanded), 'mt-4')}>{secondary.map(entry => item(entry))}</ul> : null}
          {signedIn.length > 0 ? (
            <ul aria-label="Your account" className={cn(listClass(expanded), 'mt-4')}>
              {signedIn.map(entry => item(entry, entry.badge === 'rewards' && claimable))}
            </ul>
          ) : null}
          <div className={cn('mt-4 flex', !expanded && 'justify-center')}>
            {expanded ? createLink : <Tooltip content="Create coin" align="start">{createLink}</Tooltip>}
          </div>
          <div className="min-h-4 flex-1" />
          <ul data-rail-group="utility" aria-label="Help and settings" className={listClass(expanded)}>
            {bottom.map(entry => item(entry))}
            <li className={cn(!expanded && 'flex justify-center')}>
              {expanded ? panelToggle : <Tooltip content="Expand sidebar" align="start">{panelToggle}</Tooltip>}
            </li>
          </ul>
        </nav>
      </aside>
    </>
  );
}
