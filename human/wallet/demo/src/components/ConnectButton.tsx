'use client';

import { useMemo, useState } from 'react';
import { useFundedAccount, useSession, useStandardAccount } from '@paxeer/wallet/react';
import { paxeerWallet } from '@/lib/paxeer';
import { truncateAddress } from '@/lib/format';
import { WalletModal } from './WalletModal';

/**
 * The single component a partner Paxeer Next.js app needs to render.
 *
 * Two visual states:
 *   - signed out         -> primary blue "Connect Paxeer" CTA
 *   - signed in w/ wallet -> neutral surface pill with mono address + ●
 *
 * Both open the same modal; the modal handles every flow inside.
 */
export function ConnectButton() {
  const paxeer = useMemo(() => paxeerWallet(), []);
  const { user, loading } = useSession(paxeer);
  // Passive reads only — NEVER auto-provision a wallet from this surface.
  // The user must explicitly pick standard vs funded inside the modal.
  const { data: standard } = useStandardAccount(paxeer);
  const { data: funded } = useFundedAccount(paxeer);
  const [open, setOpen] = useState(false);

  const signedIn = !!user;
  const standardAddress = standard?.wallet.address ?? null;
  const fundedAddress = funded?.wallet.address ?? null;
  // Prefer the standard address in the pill if it exists; otherwise fall
  // back to the funded address (with a FUNDED hint badge). If neither yet,
  // we still show a generic "connected" pill so the user knows they're in.
  const displayAddress = standardAddress ?? fundedAddress;
  const onlyFunded = !standardAddress && !!fundedAddress;

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        disabled={loading}
        className={
          signedIn
            ? // Connected state — monochrome pill
              `
                inline-flex items-center gap-2.5
                rounded-full border border-neutral-700 bg-neutral-800
                px-4 py-2.5
                transition-colors duration-[var(--duration-snappy)]
                ease-[var(--ease-standard)]
                hover:border-neutral-600 hover:bg-neutral-700
              `
            : // Disconnected — primary Paxeer Blue CTA
              `
                inline-flex items-center gap-2
                rounded-full bg-[#004ced] px-5 py-2.5
                text-[14px] text-white
                transition-[background,opacity]
                duration-[var(--duration-snappy)]
                ease-[var(--ease-standard)]
                hover:bg-[#0040c9]
                disabled:cursor-not-allowed disabled:opacity-50
              `
        }
      >
        {signedIn ? (
          <>
            <span className="inline-block h-2 w-2 rounded-full bg-[#05c168]" />
            {displayAddress ? (
              <span className="font-mono text-[13px] text-neutral-100">
                {truncateAddress(displayAddress)}
              </span>
            ) : (
              <span className="text-[13px] text-neutral-100">Connected</span>
            )}
            {onlyFunded && (
              <span
                className="
                  rounded-full border border-[#8FA8FF]/30 bg-[#8FA8FF]/10
                  px-1.5 py-0.5 font-mono text-[10px] uppercase tracking-wider
                  text-[#8FA8FF]
                "
              >
                Funded
              </span>
            )}
          </>
        ) : (
          <>
            <PaxeerMark />
            <span>{loading ? 'Loading…' : 'Connect Paxeer'}</span>
          </>
        )}
      </button>

      <WalletModal open={open} onOpenChange={setOpen} />
    </>
  );
}

function PaxeerMark() {
  // Minimal mark — a square punctured by a slash. Fills with white on the
  // primary CTA, swappable to your real logo SVG when the brand asset lands.
  return (
    <svg width="14" height="14" viewBox="0 0 14 14" fill="none" aria-hidden="true">
      <rect x="1" y="1" width="12" height="12" rx="2" fill="currentColor" opacity="0.15" />
      <path
        d="M3 11L11 3"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
      />
    </svg>
  );
}
