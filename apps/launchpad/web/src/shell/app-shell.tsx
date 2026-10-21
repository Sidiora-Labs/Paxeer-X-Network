'use client';

import { type CSSProperties, type ReactNode, useState, useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { usePathname } from 'next/navigation';
import { clearPrivateFoundationCache } from '@/core/app-foundation/private-cache';
import { routeForPath, safeExternalDestination } from '@/core/app-foundation/navigation';
import '@/core/app-foundation/design-tokens.css';
import { installNavigationHistory } from '@/core/app-foundation/history-context';
import { Button } from '@/ui/primitives';
import { SquiCircleFilterStatic } from '@/new-components/SkiperSquiCircleFilterLayout';
import { useWalletToasts } from '@/hooks/ui/use-wallet-toasts';
import { cn } from '@/lib/utils';
import SidebarNav, { useRailState } from './sidebar-nav';
import TopBar from './top-bar';
import TickerStrip from './ticker-strip';
import MobileBottomNav from './mobile-bottom-nav';
import { SHELL_TOP_VARS } from './nav-config';
import { ShellFooter } from './platform-metrics';

export default function AppShell({ children }: { children: ReactNode }) {
  const pathname = usePathname();
  const queryClient = useQueryClient();
  const [privateEpoch, setPrivateEpoch] = useState(0);
  const [linkBlocked, setLinkBlocked] = useState(false);
  const previousPath = useRef(pathname);
  const { expanded: railExpanded } = useRailState();
  const topRef = useRef<HTMLDivElement>(null);
  const [topHeight, setTopHeight] = useState<number | null>(null);

  useEffect(() => {
    const element = topRef.current;
    if (!element) return;
    const update = () => setTopHeight(element.offsetHeight);
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (previousPath.current === pathname) return;
    document.getElementById('main-content')?.focus({ preventScroll: true });
    previousPath.current = pathname;
  }, [pathname]);

  useEffect(() => {
    const clear = () => setPrivateEpoch(epoch => epoch + 1);
    const restored = (event: PageTransitionEvent) => { if (event.persisted) void clearPrivateFoundationCache(queryClient); };
    window.addEventListener('kindle:private-state-cleared', clear);
    window.addEventListener('pageshow', restored);
    return () => { window.removeEventListener('kindle:private-state-cleared', clear); window.removeEventListener('pageshow', restored); };
  }, [queryClient]);
  useEffect(() => {
    const guard = (event: MouseEvent) => {
      const anchor = event.target instanceof Element ? event.target.closest('a[href]') : null;
      if (!(anchor instanceof HTMLAnchorElement)) return;
      let target: URL;
      try { target = new URL(anchor.href); } catch { event.preventDefault(); event.stopPropagation(); setLinkBlocked(true); return; }
      if (target.origin === window.location.origin && target.protocol === window.location.protocol && !target.username && !target.password) return;
      const approved = safeExternalDestination(anchor.href);
      if (!approved) { event.preventDefault(); event.stopPropagation(); setLinkBlocked(true); return; }
      anchor.href = approved;
      anchor.rel = 'noopener noreferrer';
      anchor.referrerPolicy = 'no-referrer';
    };
    document.addEventListener('click', guard, true);
    document.addEventListener('auxclick', guard, true);
    return () => { document.removeEventListener('click', guard, true); document.removeEventListener('auxclick', guard, true); };
  }, []);
  useEffect(() => installNavigationHistory(), []);
  useEffect(() => { window.dispatchEvent(new Event('kindle:navigation-complete')); }, [pathname]);
  useWalletToasts();

  const route = routeForPath(pathname);
  // --shell-top is the fixed header stack height: measured once mounted, and
  // before that the CSS default (top bar, plus the ticker from md up).
  const shellTop = topHeight === null ? undefined : ({ '--shell-top': `${topHeight}px` } as CSSProperties);

  return (
    <div
      data-shell-pathname={pathname}
      data-shell-route-id={route?.id || 'unknown'}
      style={shellTop}
      className={cn('foundation-shell min-w-0 max-w-full bg-canvas text-text', SHELL_TOP_VARS)}
    >
      <a href="#main-content" className="sr-only focus:not-sr-only fixed left-0 top-0 z-[var(--sl-z-tooltip)] rounded-sm bg-surface-3 p-3 text-sm text-text">Skip to content</a>
      <SquiCircleFilterStatic />
      <SidebarNav />
      <div className={cn('flex min-h-screen flex-col transition-[margin] duration-base md:ml-rail-width', railExpanded && 'xl:ml-rail-expanded-width')}>
        <div
          ref={topRef}
          className={cn('fixed left-0 right-0 top-0 z-[var(--sl-z-header)] flex flex-col bg-canvas transition-[left] duration-base md:left-rail-width', railExpanded && 'xl:left-rail-expanded-width')}
        >
          <TopBar />
          <TickerStrip />
        </div>

        <main
          id="main-content"
          tabIndex={-1}
          data-shell-main=""
          // Sticky-footer layout: main is at least one viewport tall (its
          // padding covers the header stack and, below md, the bottom tab bar),
          // the page grows into the free space and the slim footer row sits at
          // the viewport bottom on short pages, or after the content on long ones.
          className="flex min-h-dvh min-w-0 flex-1 flex-col overflow-x-clip pb-14 pt-[var(--shell-top)] md:pb-0"
        >
          {linkBlocked ? (
            <p role="alert" className="flex items-center gap-2 bg-warning-soft px-4 py-2 text-sm text-text">
              This link can't be opened
              <Button variant="ghost" size="sm" onClick={() => setLinkBlocked(false)}>Dismiss</Button>
            </p>
          ) : null}
          {route ? (
            <div key={privateEpoch} data-shell-content="" className="flex min-w-0 flex-1 flex-col">{children}</div>
          ) : (
            <section data-shell-content="" className="flex flex-1 flex-col items-start gap-3 p-6">
              <h1 className="text-xl font-extrabold text-text">Page not found</h1>
              <p className="text-sm text-text-muted">We couldn't find that page</p>
              <Button href="/" variant="secondary" size="md">Go home</Button>
            </section>
          )}
          {route?.id === 'terminal' ? null : <ShellFooter />}
        </main>
      </div>
      <MobileBottomNav />
    </div>
  );
}
