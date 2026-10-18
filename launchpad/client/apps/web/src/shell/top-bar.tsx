'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import Image from 'next/image';
import { usePathname, useRouter } from 'next/navigation';
import { ChevronLeft, Plus } from 'lucide-react';
import ErrorBoundary from '@/ui/common/error-boundary';
import { IconButton } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import GlobalSearch from './global-search';
import WalletButton from './wallet-button';
import PasteAddress from './paste-address';

const DEPTH_KEY = 'sidioraShellDepth';

/**
 * Tracks how deep the viewer is inside in-app history. Each new entry gets a
 * depth stamped into history.state; entries reached by back/forward keep
 * theirs, so a depth above zero means history.back() stays inside the app.
 */
function useInAppHistory(pathname: string): boolean {
  const [canGoBack, setCanGoBack] = useState(false);
  const previous = useRef<number | null>(null);
  useEffect(() => {
    const state: unknown = window.history.state;
    const stamped = state && typeof state === 'object' ? (state as Record<string, unknown>)[DEPTH_KEY] : undefined;
    if (typeof stamped === 'number' && Number.isSafeInteger(stamped) && stamped >= 0) {
      previous.current = stamped;
      setCanGoBack(stamped > 0);
      return;
    }
    const depth = previous.current === null ? 0 : previous.current + 1;
    if (state === null || typeof state === 'object') {
      window.history.replaceState({ ...((state as Record<string, unknown> | null) ?? {}), [DEPTH_KEY]: depth }, '', window.location.href);
    }
    previous.current = depth;
    setCanGoBack(depth > 0);
  }, [pathname]);
  return canGoBack;
}

const squareLink = 'inline-flex size-8 shrink-0 items-center justify-center rounded-sm bg-surface-1 text-text transition-colors duration-fast hover:bg-surface-hover active:bg-surface-active [&_svg]:size-icon-md';

/**
 * Search trigger density. Desktop gets the reference's 32px field on a subtle
 * surface-1 fill; touch widths get an icon-only ghost trigger that opens the
 * same palette, which carries its own input.
 */
const searchField = '[&>button]:h-8 [&>button]:bg-surface-1 [&>button]:hover:bg-surface-hover';
const searchIcon = '[&>button]:size-9 [&>button]:bg-transparent [&>button]:text-text [&>button]:hover:bg-surface-hover';

/**
 * Top bar anatomy, following the reference at each width:
 * - phone (< md): logo, then paste, search icon and sign in on the right;
 *   create lives in the bottom tab bar.
 * - tablet (md to lg): the rail carries the logo; back, then paste, search
 *   icon, create and sign in on the right.
 * - desktop (lg and up): 32px search field with the ghost paste icon beside
 *   it, create and sign in on the right; no back button.
 */
export default function TopBar() {
  const pathname = usePathname();
  const router = useRouter();
  const canGoBack = useInAppHistory(pathname);
  const atHome = pathname === '/';

  const back = () => {
    if (canGoBack) router.back();
    else router.push('/');
  };

  return (
    <header className="flex h-header-height items-center gap-1 bg-canvas px-3 sm:px-4 lg:gap-2">
      <Link href="/" aria-label="Sidiora Launch home" className="flex shrink-0 items-center rounded-sm p-1 md:hidden">
        <Image src="/Kindle-Launch-logo-dark.png" alt="" width={28} height={28} className="size-7" />
      </Link>
      {atHome && !canGoBack ? null : (
        <IconButton
          className="hidden md:inline-flex lg:hidden"
          label={canGoBack ? 'Back' : 'Back to home'}
          icon={<ChevronLeft />}
          variant="ghost"
          size="md"
          onClick={back}
        />
      )}
      <div data-search-field="" className={cn('hidden min-w-0 flex-1 lg:block lg:max-w-[480px]', searchField)}>
        <ErrorBoundary area="global-search"><GlobalSearch /></ErrorBoundary>
      </div>
      <div className="flex-1 lg:hidden" />
      <PasteAddress />
      <div data-search-icon="" className={cn('shrink-0 lg:hidden', searchIcon)}>
        <ErrorBoundary area="global-search"><GlobalSearch compact /></ErrorBoundary>
      </div>
      <div className="hidden flex-1 lg:block" />
      <Link href="/create" aria-label="Create coin" title="Create coin" className={cn(squareLink, 'hidden md:inline-flex')}>
        <Plus aria-hidden="true" />
      </Link>
      <div className="ml-1 flex shrink-0 items-center gap-2">
        <ErrorBoundary area="widget"><WalletButton /></ErrorBoundary>
      </div>
    </header>
  );
}
