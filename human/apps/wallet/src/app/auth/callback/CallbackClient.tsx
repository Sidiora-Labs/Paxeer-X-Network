'use client';

/**
 * `/auth/callback` — landing page for Supabase OAuth and magic-link
 * redirects. Marked dynamic because (a) the page reads URL hash params
 * the Supabase JS client injects after redirect, which Next.js can't
 * pre-render, and (b) build-time SSG runs without Supabase env vars in
 * many CI environments, leaving `EmbeddedWalletProvider` unmounted and
 * causing `useEmbeddedWallet()` to throw during prerender.
 *
 * Flow:
 *   1. User signs in with Google/Apple/X/email magic-link via
 *      `EmbeddedSignIn`. Supabase redirects them here with the auth
 *      token in the URL fragment.
 *   2. The Supabase JS client (configured with `detectSessionInUrl: true`)
 *      automatically consumes the URL params and creates a session.
 *   3. We subscribe to `onAuthStateChange`; when the session lands, we
 *      flag the user's chosen custody model as `'embedded'` in
 *      `WalletKindProvider` and route them to the wallet shell.
 *
 * If the session never lands within 8 seconds we surface a friendly
 * error with a "Try again" link back to onboarding. Common causes:
 *   - URL was opened in a different browser than the one that started
 *     the flow (Supabase session is in localStorage on the original).
 *   - OAuth consent screen was dismissed.
 *   - Supabase Redirect URL allowlist is missing this origin.
 */

import { useCallback, useEffect, useRef, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
    EmbeddedWalletProvider,
    useOptionalEmbeddedWallet,
} from '@/lib/wallet';
import {
    WalletKindProvider,
    useWalletKind,
} from '@/providers/WalletKindProvider';
import { Loader2 } from 'lucide-react';

const TIMEOUT_MS = 8_000;

/**
 * `/auth/callback` is a sibling route to `/`, so it does NOT inherit the
 * provider tree mounted inside `app/page.tsx` (which only wraps the
 * default route's subtree). We mount the providers this page actually
 * touches — `EmbeddedWalletProvider` for the Supabase session listener
 * and `WalletKindProvider` for promoting the user to embedded custody —
 * locally here.
 *
 * The alternative is hoisting both providers into `app/layout.tsx`, but
 * that would force every page (including SSR-only routes) to instantiate
 * the embedded wallet client. Local wrapping is the minimal fix.
 */
export default function CallbackClient() {
    return (
        <EmbeddedWalletProvider>
            <WalletKindProvider>
                <CallbackInner />
            </WalletKindProvider>
        </EmbeddedWalletProvider>
    );
}

function CallbackInner() {
    const router = useRouter();
    // `useOptionalEmbeddedWallet` returns `null` (rather than throwing) when
    // the provider isn't configured. That makes the page safe to render
    // during build-time prerender / SSR, and lets us show a friendly
    // "embedded wallet not configured" error at runtime if env vars are
    // missing in production.
    const embedded = useOptionalEmbeddedWallet();
    const { setKind } = useWalletKind();
    const [error, setError] = useState<string | null>(null);

    const handleRetry = useCallback(() => {
        router.replace('/');
    }, [router]);

    useEffect(() => {
        if (!embedded?.wallet) {
            setError('Embedded wallet is not configured.');
            return;
        }
        let alive = true;

        // If a session is already present (SDK consumed the URL synchronously),
        // promote immediately and bail.
        if (embedded.session) {
            setKind('embedded');
            router.replace('/');
            return;
        }

        const unsub = embedded.wallet.onAuthStateChange((_event, session) => {
            if (!alive) return;
            if (session) {
                setKind('embedded');
                router.replace('/');
            }
        });

        const t = setTimeout(() => {
            if (!alive) return;
            setError('Sign-in did not complete. Please try again.');
        }, TIMEOUT_MS);

        return () => {
            alive = false;
            clearTimeout(t);
            unsub();
        };
        // We only want to run this once per mount; subsequent embedded
        // re-renders (loading state changes, etc.) shouldn't restart the
        // listener.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    return (
        <div className="min-h-screen flex items-center justify-center px-6">
            <div className="flex flex-col items-center gap-4 max-w-sm text-center">
                {error ? (
                    <>
                        <div className="w-12 h-12 rounded-full bg-red-500/10 flex items-center justify-center text-red-400 text-xl">
                            !
                        </div>
                        <p className="text-sm text-white">Sign-in failed</p>
                        <p className="text-xs text-pax-muted">{error}</p>
                        <button
                            onClick={handleRetry}
                            className="mt-2 px-4 py-2 rounded-xl bg-pax-accent text-black text-xs font-semibold press-scale"
                        >
                            Try again
                        </button>
                    </>
                ) : (
                    <>
                        <Loader2
                            aria-label="Finishing sign-in"
                            className="h-10 w-10 animate-spin text-pax-accent"
                        />
                        <p className="text-sm text-pax-muted">Finishing sign-in…</p>
                    </>
                )}
            </div>
        </div>
    );
}
