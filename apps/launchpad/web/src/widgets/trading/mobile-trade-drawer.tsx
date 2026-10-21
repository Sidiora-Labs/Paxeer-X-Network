'use client';

import { useState, useEffect, useRef } from 'react';
import { X } from 'lucide-react';
import { Button, IconButton } from '@/ui/primitives';
import { openAuthModal } from '@/shell/auth-modal-store';
import { useViewerWallet } from '@/widgets/social/use-follow';
import TradePanel from './trade-panel';

interface MobileTradeDrawerProps {
  poolAddress: string;
}

/**
 * Below lg the trade panel lives in a bottom drawer behind a sticky CTA: above the bottom nav below
 * md, and at md (no bottom nav, the left rail is visible) at the foot of the content column. Signed
 * out, the CTA reads "Sign in to trade" and opens the sign-in flow directly (pump.fun's "Log in to
 * trade"); signed in, it opens the drawer.
 */
export default function MobileTradeDrawer({ poolAddress }: MobileTradeDrawerProps) {
  const [open, setOpen] = useState(false);
  const viewer = useViewerWallet();
  const drawer = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const previous = document.activeElement as HTMLElement | null;
    drawer.current?.querySelector<HTMLElement>('button,input')?.focus();
    return () => previous?.focus();
  }, [open]);

  useEffect(() => {
    document.body.style.overflow = open ? 'hidden' : '';
    return () => { document.body.style.overflow = ''; };
  }, [open]);

  return (
    <>
      <div data-mobile-trade-cta={viewer ? 'trade' : 'sign-in'} className="fixed inset-x-0 bottom-[72px] z-40 px-4 md:bottom-4 md:left-rail-width lg:hidden" style={{ paddingBottom: 'calc(4px + env(safe-area-inset-bottom, 0px))' }}>
        <Button
          variant="primary"
          size="lg"
          fullWidth
          pill
          aria-expanded={viewer ? open : undefined}
          aria-controls={viewer ? 'mobile-trade-drawer' : undefined}
          onClick={() => { if (viewer) setOpen(true); else openAuthModal('trade'); }}
        >
          {viewer ? 'Trade' : 'Sign in to trade'}
        </Button>
      </div>

      {open ? (
        <div className="fixed inset-0 z-50 bg-scrim lg:hidden" onClick={() => setOpen(false)} aria-hidden="true" />
      ) : null}

      <div
        ref={drawer}
        id="mobile-trade-drawer"
        onKeyDown={event => {
          if (event.key === 'Escape') { event.preventDefault(); setOpen(false); return; }
          if (event.key !== 'Tab' || !open) return;
          const controls = Array.from(drawer.current?.querySelectorAll<HTMLElement>('button:not(:disabled),input:not(:disabled),select:not(:disabled),a[href]') ?? []).filter(element => element.getClientRects().length > 0);
          const first = controls[0];
          const last = controls[controls.length - 1];
          if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus(); }
          else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus(); }
        }}
        role={open ? 'dialog' : undefined}
        aria-modal={open ? true : undefined}
        aria-label={open ? 'Trade' : undefined}
        aria-hidden={!open}
        className={`fixed inset-x-0 bottom-0 z-50 md:left-rail-width transition-transform duration-slow ease-out lg:hidden ${open ? 'translate-y-0' : 'translate-y-full'}`}
      >
        <div className="max-h-[85vh] overflow-y-auto rounded-t-lg bg-surface-1" style={{ paddingBottom: 'calc(24px + env(safe-area-inset-bottom, 0px))' }}>
          <div className="flex items-center justify-center pb-1 pt-3">
            <div className="h-1 w-10 rounded-pill bg-surface-3" aria-hidden="true" />
          </div>
          <div className="flex justify-end px-3 pb-1">
            <IconButton label="Close trade drawer" icon={<X />} variant="ghost" size="md" onClick={() => setOpen(false)} />
          </div>
          <div className="px-3">
            {open ? <TradePanel poolAddress={poolAddress} /> : null}
          </div>
        </div>
      </div>
    </>
  );
}
