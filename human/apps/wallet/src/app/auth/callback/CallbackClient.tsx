'use client';

import { useCallback, useEffect, useState } from 'react';
import { useRouter } from 'next/navigation';
import { Loader2 } from 'lucide-react';
import { custodyChoiceRepository } from '@/platform/storage/repositories';
import { WalletProvider, useWallet } from '@/wallet/WalletProvider';

const TIMEOUT_MS = 8_000;

export default function CallbackClient() {
    const [armed] = useState(() => {
        custodyChoiceRepository.write('embedded');
        return true;
    });
    return armed ? (
        <WalletProvider>
            <CallbackInner />
        </WalletProvider>
    ) : null;
}

function CallbackInner() {
    const router = useRouter();
    const { status, configError, error: walletError } = useWallet();
    const [timedOut, setTimedOut] = useState(false);

    const handleRetry = useCallback(() => {
        router.replace('/');
    }, [router]);

    useEffect(() => {
        if (status === 'ready') router.replace('/');
    }, [status, router]);

    useEffect(() => {
        const timer = setTimeout(() => setTimedOut(true), TIMEOUT_MS);
        return () => clearTimeout(timer);
    }, []);

    const error = configError
        ? 'Embedded wallet is not configured.'
        : walletError ?? (status === 'signed-out' || timedOut ? 'Sign-in did not complete. Please try again.' : null);

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
