'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useAccount } from 'wagmi';
import { CircleEllipsis, CirclePlus, X, type LucideIcon } from 'lucide-react';
import { isRouteActive, routeForPath } from '@/core/app-foundation/navigation';
import { useDialogFocus } from '@/core/app-foundation/use-dialog-focus';
import { IconButton } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { createItem, navItems, type NavItem } from './nav-config';

/**
 * Phone-only bottom navigation (< md, 768). From md up the icon rail is the
 * navigation, matching the reference, which keeps its rail at 768 and shows a
 * bottom bar only on phones. Anatomy follows the reference: five icon-only
 * targets on a surface-1 bar (home, explore, create, bounties, more), the
 * active target tinted with the accent, and a "More" sheet holding every other
 * rail destination. Separation is background contrast only.
 */
export const BOTTOM_NAV_BREAKPOINT_PX = 768;

/** Bar height above the safe-area inset (`h-14`); fixed overlays below the breakpoint offset by this. */
export const BOTTOM_NAV_HEIGHT_PX = 56;

/** Bottom bar targets in order; ids refer to nav-config. Bounties is our equivalent of the reference "GO" slot. */
export const BOTTOM_NAV_IDS = ['home', 'explore', 'create', 'bounties'] as const;

function configured(id: string): NavItem {
  const item = id === createItem.id ? createItem : navItems.find(entry => entry.id === id);
  if (!item) throw new Error(`Navigation item ${id} is not configured`);
  return item;
}

export interface MoreSheetGroup {
  id: 'browse' | 'account' | 'resources';
  label: string;
  items: NavItem[];
}

/**
 * Destinations listed in the More sheet: every rail item that is not already a
 * bottom-bar target. Account items appear only with a connected wallet, the
 * same condition the rail uses for wallet-gated items.
 */
export function moreSheetGroups(walletReady: boolean): MoreSheetGroup[] {
  const pinned = new Set<string>(BOTTOM_NAV_IDS);
  const pick = (section: NavItem['section']) => navItems.filter(item => item.section === section && !pinned.has(item.id) && (item.requires === 'public' || walletReady));
  const groups: MoreSheetGroup[] = [
    { id: 'browse', label: 'Browse', items: pick('main') },
    { id: 'account', label: 'Your account', items: pick('signed-in') },
    { id: 'resources', label: 'Resources', items: pick('bottom') },
  ];
  return groups.filter(group => group.items.length > 0);
}

/** Icon overrides for the bottom bar: the reference draws create as an outlined circled plus, not a filled tile. */
const barIcon: Partial<Record<string, LucideIcon>> = { create: CirclePlus };

const targetClass = (active: boolean) => cn(
  'flex min-h-touch-target-min w-full items-center justify-center rounded-sm transition-colors duration-fast active:bg-surface-active',
  active ? 'text-accent' : 'text-text hover:text-text-muted',
);

export default function MobileBottomNav() {
  const pathname = usePathname() ?? '/';
  const { address, isConnected } = useAccount();
  const walletReady = isConnected && !!address;
  // The sheet records the path it was opened on, so any navigation closes it without an effect.
  const [openedOn, setOpenedOn] = useState<string | null>(null);
  const open = openedOn !== null && openedOn === pathname;
  const close = useCallback(() => setOpenedOn(null), []);
  const sheetRef = useRef<HTMLDivElement>(null);
  useDialogFocus(sheetRef, open);

  useEffect(() => {
    if (!open) return;
    const wide = window.matchMedia(`(min-width: ${BOTTOM_NAV_BREAKPOINT_PX}px)`);
    const closeWhenWide = () => { if (wide.matches) close(); };
    closeWhenWide();
    wide.addEventListener('change', closeWhenWide);
    return () => wide.removeEventListener('change', closeWhenWide);
  }, [open, close]);

  const groups = moreSheetGroups(walletReady);
  const moreActive = groups.some(group => group.items.some(item => isRouteActive(pathname, item.href)));
  const targets = BOTTOM_NAV_IDS.map(configured);

  return (
    <>
      <nav
        aria-label="Mobile navigation"
        className="fixed inset-x-0 bottom-0 z-[var(--sl-z-rail)] bg-surface-1 md:hidden"
        style={{ paddingBottom: 'env(safe-area-inset-bottom, 0px)' }}
      >
        <ul className="flex h-14 items-stretch px-2">
          {targets.map(item => {
            const active = isRouteActive(pathname, item.href);
            const Icon = barIcon[item.id] ?? item.icon;
            return (
              <li key={item.id} className="flex min-w-0 flex-1 items-center">
                <Link
                  href={item.href}
                  aria-label={item.label}
                  aria-current={active ? 'page' : undefined}
                  data-route-id={routeForPath(item.href)?.id}
                  className={targetClass(active)}
                >
                  <Icon aria-hidden="true" className="size-6" strokeWidth={active ? 2.25 : 2} />
                </Link>
              </li>
            );
          })}
          <li className="flex min-w-0 flex-1 items-center">
            <button
              type="button"
              aria-label="More"
              aria-haspopup="dialog"
              aria-expanded={open}
              onClick={() => setOpenedOn(open ? null : pathname)}
              className={targetClass(open || moreActive)}
            >
              <CircleEllipsis aria-hidden="true" className="size-6" strokeWidth={open || moreActive ? 2.25 : 2} />
            </button>
          </li>
        </ul>
      </nav>

      {open ? (
        <div className="fixed inset-0 z-[var(--sl-z-modal)] md:hidden">
          <button type="button" aria-label="Close menu" tabIndex={-1} onClick={close} className="absolute inset-0 bg-[var(--sl-tint-scrim)]" />
          <div
            ref={sheetRef}
            role="dialog"
            aria-modal="true"
            aria-label="More"
            onKeyDown={event => { if (event.key === 'Escape') { event.stopPropagation(); close(); } }}
            className="absolute inset-x-0 bottom-0 flex max-h-[80dvh] flex-col rounded-t-lg bg-surface-2"
            style={{ paddingBottom: 'env(safe-area-inset-bottom, 0px)' }}
          >
            <div aria-hidden="true" className="mx-auto mt-2 h-1 w-8 shrink-0 rounded-pill bg-surface-3" />
            <div className="flex shrink-0 items-center justify-between px-4 pt-1">
              <h2 className="text-md font-bold text-text">More</h2>
              <IconButton label="Close menu" icon={<X />} variant="ghost" size="md" onClick={close} />
            </div>
            <div className="flex min-h-0 flex-col gap-3 overflow-y-auto px-2 pb-3 pt-1">
              {groups.map(group => (
                <section key={group.id} aria-label={group.label} className="flex flex-col">
                  {group.id === 'browse' ? null : <h3 className="px-3 pb-1 text-2xs font-semibold uppercase text-text-faint">{group.label}</h3>}
                  <ul className="flex flex-col">
                    {group.items.map(item => {
                      const active = isRouteActive(pathname, item.href);
                      const Icon = item.icon;
                      return (
                        <li key={item.id}>
                          <Link
                            href={item.href}
                            aria-current={active ? 'page' : undefined}
                            data-route-id={routeForPath(item.href)?.id}
                            onClick={close}
                            className={cn(
                              'flex h-12 items-center gap-3 rounded-sm px-3 text-base font-semibold transition-colors duration-fast active:bg-surface-active',
                              active ? 'bg-surface-3 text-accent' : 'text-text hover:bg-surface-hover',
                            )}
                          >
                            <Icon aria-hidden="true" className="size-icon-lg shrink-0" />
                            <span className="truncate">{item.label}</span>
                          </Link>
                        </li>
                      );
                    })}
                  </ul>
                </section>
              ))}
            </div>
          </div>
        </div>
      ) : null}
    </>
  );
}
