'use client';

import { useState, useEffect, useRef } from 'react';
import Link from 'next/link';
import { useDialogFocus } from '@/core/app-foundation/use-dialog-focus';

/** localStorage key recording that the visitor accepted the platform rules. */
export const RULES_AGREED_KEY = 'sidiora_rules_agreed_v1';
/** Window event fired once the visitor accepts; the cookie banner waits for it. */
export const RULES_AGREED_EVENT = 'sidiora:rules-agreed';

/** True once the visitor has accepted the platform rules on this device. */
export function rulesAgreed(): boolean {
  try { return Boolean(window.localStorage.getItem(RULES_AGREED_KEY)); } catch { return false; }
}

const RULES: { text: React.ReactNode }[] = [
  {
    text: (
      <>
        User-generated tokens are <strong className="text-text">not endorsed</strong> by
        Sidiora. Always do your own research before buying, selling, or holding any Digital Asset.
      </>
    ),
  },
  {
    text: (
      <>
        Smart contracts carry inherent risk. You may experience{' '}
        <span className="text-danger">partial or total loss of funds</span>. Use only what you
        can afford to lose.
      </>
    ),
  },
  {
    text: (
      <>
        Market manipulation, pump-and-dump schemes, wash trading, and deceptive conduct are{' '}
        <strong className="text-text">strictly prohibited</strong>.
      </>
    ),
  },
  {
    text: (
      <>
        No harassment, hate speech, impersonation, or synthetic/AI-generated voice misuse in the
        Voice Chat Feature.
      </>
    ),
  },
  {
    text: (
      <>
        Access from sanctioned or prohibited jurisdictions (including Russia, Iran, North Korea,
        Cuba, and Syria) is not permitted.
      </>
    ),
  },
  {
    text: (
      <>
        By continuing, you confirm you have read and accept our{' '}
        <Link href="/docs/terms" className="text-accent underline hover:opacity-80 transition">
          Terms of Service
        </Link>{' '}
        and{' '}
        <Link href="/docs/privacy" className="text-accent underline hover:opacity-80 transition">
          Privacy Policy
        </Link>
        .
      </>
    ),
  },
];

export default function PlatformRulesModal() {
  const [visible, setVisible] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!rulesAgreed()) setVisible(true);
  }, []);

  // Required consent: focus stays inside the dialog and the page behind is inert.
  useDialogFocus(dialogRef, visible);

  if (!visible) return null;

  function handleAgree() {
    try { window.localStorage.setItem(RULES_AGREED_KEY, Date.now().toString()); } catch { /* storage blocked: accept for this page view only */ }
    setVisible(false);
    window.dispatchEvent(new Event(RULES_AGREED_EVENT));
  }

  return (
    <div className="fixed inset-0 z-[9999] flex items-center justify-center bg-scrim/70 p-4">
      <div
        ref={dialogRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby="platform-rules-title"
        aria-describedby="platform-rules-summary"
        // Acceptance is required, so Escape must not dismiss it.
        onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); } }}
        className="flex w-full max-w-[520px] max-h-[calc(100dvh-2rem)] flex-col overflow-hidden rounded-2xl bg-surface-1"
      >
        <div data-slot="dialog-body" className="min-h-0 flex-1 overflow-y-auto overscroll-contain">
          <header data-slot="dialog-banner" className="bg-surface-2 px-6 pb-7 pt-8 text-center">
            <p className="text-sm font-bold text-text-muted">
              Launch, earn, <span className="text-accent">enjoy</span>
            </p>
            <p className="mt-1 text-[32px] font-extrabold leading-none tracking-tight text-text">Sidiora</p>
          </header>

          <div className="px-6 pt-5">
            <div className="mb-5 flex items-center gap-3">
              <div className="w-9 h-9 rounded-xl bg-accent-soft flex items-center justify-center flex-shrink-0">
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  className="text-accent"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  aria-hidden="true"
                >
                  <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                </svg>
              </div>
              <div>
                <h2 id="platform-rules-title" className="font-bold text-[17px] text-text leading-tight">
                  Platform rules
                </h2>
                <p id="platform-rules-summary" className="text-2xs text-text-faint mt-0.5">
                  Please review before continuing
                </p>
              </div>
            </div>

            <ul className="pb-5 space-y-3">
              {RULES.map((rule, i) => (
                <li key={i} className="flex items-start gap-3">
                  <span aria-hidden="true" className="mt-[6px] block h-[5px] w-[5px] shrink-0 rounded-full bg-accent" />
                  <span className="text-xs text-text-muted leading-[1.65]">{rule.text}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>

        <footer data-slot="dialog-footer" className="shrink-0 bg-surface-1 px-6 pb-5 pt-3">
          <button
            type="button"
            onClick={handleAgree}
            className="w-full rounded-xl bg-accent py-3 text-sm font-bold text-on-accent transition-colors hover:bg-accent-hover active:bg-accent-active"
          >
            I understand and agree
          </button>

          <p className="mt-3 text-center text-2xs text-text-faint">
            You'll only see this once. All policies are in the site footer
          </p>
        </footer>
      </div>
    </div>
  );
}
