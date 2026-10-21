'use client';

import { useState, useEffect, useSyncExternalStore } from 'react';
import Link from 'next/link';
import { BOTTOM_NAV_BREAKPOINT_PX } from '@/shell/mobile-bottom-nav';
import { RULES_AGREED_EVENT, rulesAgreed } from './platform-rules-modal';

const COOKIE_KEY = 'sidiora_cookie_consent';

/**
 * Below the bottom-nav breakpoint the phone tab bar is pinned to the viewport
 * bottom, so the banner sits directly above it (bar height plus the safe-area
 * inset the bar pads with) and never covers its targets. From the breakpoint
 * up there is no bar and the banner keeps its flush-bottom placement.
 */
export const COOKIE_BANNER_PHONE_BOTTOM_CLASS = 'bottom-[calc(56px+env(safe-area-inset-bottom,0px))]';
export const COOKIE_BANNER_WIDE_BOTTOM_CLASS = 'bottom-0';
// 56 is BOTTOM_NAV_HEIGHT_PX written as a literal so Tailwind's scanner emits the class; cookie-banner.test.tsx keeps the two in step.

const PHONE_QUERY = `(max-width: ${BOTTOM_NAV_BREAKPOINT_PX - 0.02}px)`;

function subscribePhone(onChange: () => void) {
  if (typeof window.matchMedia !== 'function') return () => {};
  const query = window.matchMedia(PHONE_QUERY);
  query.addEventListener('change', onChange);
  return () => query.removeEventListener('change', onChange);
}
const phoneSnapshot = () => typeof window.matchMedia === 'function' && window.matchMedia(PHONE_QUERY).matches;
const serverSnapshot = () => false;

export default function CookieBanner() {
  const [visible, setVisible] = useState(false);
  const phone = useSyncExternalStore(subscribePhone, phoneSnapshot, serverSnapshot);

  // The platform rules modal is the first-visit consent; the cookie banner
  // waits until it has been accepted so the two never stack on screen.
  useEffect(() => {
    let pending = false;
    try { pending = !window.localStorage.getItem(COOKIE_KEY); } catch { pending = true; }
    if (!pending) return;
    if (rulesAgreed()) { setVisible(true); return; }
    const show = () => setVisible(true);
    window.addEventListener(RULES_AGREED_EVENT, show);
    return () => window.removeEventListener(RULES_AGREED_EVENT, show);
  }, []);

  if (!visible) return null;

  const choose = (value: 'accepted' | 'declined') => {
    try { window.localStorage.setItem(COOKIE_KEY, value); } catch { /* storage blocked: hide for this page view */ }
    setVisible(false);
  };
  const accept = () => choose('accepted');
  const decline = () => choose('declined');

  return (
    <div
      data-placement={phone ? 'above-bottom-nav' : 'viewport-bottom'}
      className={`fixed left-0 right-0 z-[9997] bg-surface-1 px-4 py-3 ${phone ? COOKIE_BANNER_PHONE_BOTTOM_CLASS : COOKIE_BANNER_WIDE_BOTTOM_CLASS}`}
    >
      <div className="max-w-4xl mx-auto flex flex-col sm:flex-row items-start sm:items-center gap-3">
        <div className="flex items-start gap-2.5 flex-1">
          <svg
            width="15"
            height="15"
            viewBox="0 0 24 24"
            fill="none"
            className="shrink-0 mt-[1px] text-text-muted"
            stroke="currentColor"
            strokeWidth="1.5"
          >
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm-1-13h2v6h-2zm0 8h2v2h-2z" />
          </svg>
          <p className="text-xs text-text-muted leading-[1.5]">
            We use cookies to improve your experience and see how the site is used. See our{' '}
            <Link href="/docs/privacy" className="text-accent underline hover:opacity-80 transition">
              privacy policy
            </Link>{' '}
            for details
          </p>
        </div>
        <div className="flex items-center gap-2 flex-shrink-0 ml-5 sm:ml-0">
          <button
            onClick={decline}
            className="px-4 py-1.5 rounded-lg bg-surface-2 text-xs text-text-faint hover:bg-surface-hover hover:text-text transition"
          >
            Decline
          </button>
          <button
            onClick={accept}
            className="px-4 py-1.5 rounded-lg bg-accent text-xs font-bold text-on-accent hover:opacity-90 transition"
          >
            Accept
          </button>
        </div>
      </div>
    </div>
  );
}
