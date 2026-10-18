'use client';

import { useEffect, useRef, useState } from 'react';
import Link from 'next/link';
import { useRouter } from 'next/navigation';
import { useAccount } from 'wagmi';
import { openAuthModal } from '@/shell/auth-modal-store';
import { readReturnPath, useEmbeddedAutoConnect } from '@/shell/embedded-sign-in';
import { usePaxeerOptional } from '@/core/wallet-sdk/embedded_sdk/embedded';

/**
 * Landing page for Paxeer Embedded Wallet redirects (magic link and OAuth).
 * Supabase consumes the URL fragment and installs the session; this page
 * waits for the wallet record, connects wagmi to the embedded connector,
 * returns the user to where they started and reopens the sign-in dialog at
 * the signature step.
 */
export default function AuthCallbackPage() {
  const router = useRouter();
  const paxeer = usePaxeerOptional();
  const { isConnected } = useAccount();
  const auto = useEmbeddedAutoConnect(Boolean(paxeer?.wallet));
  const [timedOut, setTimedOut] = useState(false);
  const finished = useRef(false);

  useEffect(() => {
    const timer = window.setTimeout(() => setTimedOut(true), 20_000);
    return () => window.clearTimeout(timer);
  }, []);

  useEffect(() => {
    if (finished.current || !paxeer?.isAuthenticated || !isConnected) return;
    finished.current = true;
    const target = readReturnPath();
    router.replace(target);
    openAuthModal('Sign one message to finish signing in');
  }, [paxeer?.isAuthenticated, isConnected, router]);

  const unavailable = !paxeer || !paxeer.wallet;
  const failed = unavailable || timedOut || auto.error || (paxeer && !paxeer.isLoading && !paxeer.session && !paxeer.isAuthenticated && timedOut);
  const message = unavailable
    ? 'Email sign-in is not available yet. Go back and use a browser wallet'
    : auto.error || timedOut || paxeer.authError ? "Couldn't finish signing in. Request a new link and try again" : null;

  return (
    <div className="min-h-[60vh] flex items-center justify-center px-4">
      <div className="rounded-lg bg-surface-1 p-6 max-w-sm w-full text-center" role="status" aria-live="polite">
        <h1 className="text-sm font-bold text-text">{failed ? "Couldn't sign you in" : 'Finishing sign-in'}</h1>
        <p className="mt-2 text-xs text-text-muted">{message ?? (auto.connecting ? 'Connecting your wallet...' : 'Signing you in...')}</p>
        {failed ? (
          <Link href="/" className="mt-4 inline-block rounded-md bg-accent px-4 py-2 text-xs font-bold text-on-accent hover:bg-accent-hover focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus">Go home</Link>
        ) : null}
      </div>
    </div>
  );
}
