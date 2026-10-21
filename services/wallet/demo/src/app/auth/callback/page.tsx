'use client';

import { useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { paxeerWallet } from '@/lib/paxeer';

/**
 * Supabase OAuth + magic-link landing page.
 *
 * The Supabase JS client (with `detectSessionInUrl: true`, default) will
 * automatically pick up the `code` / `access_token` from the URL on mount,
 * exchange it for a session, persist it to localStorage, and fire
 * `onAuthStateChange`. We just need to mount and then redirect.
 */
export default function AuthCallback() {
  const router = useRouter();
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    const paxeer = paxeerWallet();

    void (async () => {
      // Give the SDK a tick to consume the URL params.
      const { data, error } = await paxeer.supabase.auth.getSession();
      if (cancelled) return;
      if (error) {
        setError(error.message);
        return;
      }
      if (data.session) {
        router.replace('/');
      } else {
        // No session yet — wait for the auth listener to fire once.
        const unsub = paxeer.onAuthStateChange((_e, session) => {
          if (session) {
            unsub();
            router.replace('/');
          }
        });
        // Safety: bail out if nothing arrives within 8s.
        setTimeout(() => {
          if (!cancelled) {
            unsub();
            setError('Sign-in did not complete. Please try again.');
          }
        }, 8000);
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [router]);

  return (
    <main className="flex min-h-dvh flex-col items-center justify-center gap-3 px-6 text-center">
      {error ? (
        <>
          <p className="text-[15px] text-neutral-100">Sign-in failed</p>
          <p className="text-[13px] text-[#ff5a65]">{error}</p>
          <a
            href="/"
            className="
              mt-2 rounded-xl border border-neutral-700 bg-neutral-800
              px-4 py-2 text-[13px] text-neutral-100
              hover:border-neutral-600 hover:bg-neutral-700
            "
          >
            Back to home
          </a>
        </>
      ) : (
        <>
          <svg
            className="h-6 w-6 animate-spin text-neutral-500"
            viewBox="0 0 24 24"
            fill="none"
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
            <path fill="currentColor" d="M4 12a8 8 0 018-8v3a5 5 0 00-5 5H4z" />
          </svg>
          <p className="text-[13px] text-neutral-400">Finishing sign-in…</p>
        </>
      )}
    </main>
  );
}
