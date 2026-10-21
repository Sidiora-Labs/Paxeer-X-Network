'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { EndpointClient, KernelAvailability, type KernelAvailabilityState } from '@paxeer/wallet';
import { WalletProvider } from '@/providers/WalletProvider';
import { resolveWalletConfig } from '@/wallet/config';
import { cn } from '@/lib/cn';
import { processSidRate } from './config';
import { BridgeView } from './BridgeView';
import { ExchangeView } from './ExchangeView';
import { FeesView } from './FeeChoice';
import { LaunchpadView } from './LaunchpadView';
import { SURFACE_ROUTES, type SurfaceId } from './routes';
import { WebDataView } from './WebDataView';
import { useWebDataCaps } from './useSurface';

export function useKernelState(): KernelAvailabilityState | null {
    const [state, setState] = useState<KernelAvailabilityState | null>(null);
    const availability = useMemo(() => {
        const resolved = resolveWalletConfig();
        return resolved.ok ? new KernelAvailability(new EndpointClient({ url: resolved.config.rpcUrl })) : null;
    }, []);
    useEffect(() => {
        let alive = true;
        if (!availability) {
            setState({ available: false, reason: 'not_configured', backend: null });
            return undefined;
        }
        availability.current().then(
            (current) => {
                if (alive) setState(current);
            },
            () => {
                if (alive) setState({ available: false, reason: 'unreachable', backend: null });
            },
        );
        return () => {
            alive = false;
        };
    }, [availability]);
    return state;
}

function WebDataSurface({ sidRate }: { sidRate: string | null }) {
    const { caps, refreshCaps } = useWebDataCaps();
    return <WebDataView sidRate={sidRate} caps={caps} refreshCaps={refreshCaps} />;
}

function SurfaceView({ surface, sidRate }: { surface: SurfaceId; sidRate: string | null }) {
    switch (surface) {
        case 'exchange':
            return <ExchangeView sidRate={sidRate} />;
        case 'bridge':
            return <BridgeView sidRate={sidRate} />;
        case 'launchpad':
            return <LaunchpadView sidRate={sidRate} />;
        case 'fees':
            return <FeesView sidRate={sidRate} />;
        case 'web-data':
            return <WebDataSurface sidRate={sidRate} />;
    }
}

export function SurfacePage({ surface }: { surface: SurfaceId }) {
    const sidRate = useMemo(() => processSidRate(), []);
    return (
        <WalletProvider>
            <main className="mx-auto min-h-screen max-w-lg">
                <nav aria-label="Paxeer X" className="flex gap-1 overflow-x-auto px-3 pt-3 no-scrollbar">
                    <Link href="/" className="rounded-lg bg-white/5 px-3 py-1.5 text-[11px] font-medium text-pax-muted">
                        Wallet
                    </Link>
                    {SURFACE_ROUTES.map((route) => (
                        <Link
                            key={route.id}
                            href={route.href}
                            aria-current={route.id === surface ? 'page' : undefined}
                            className={cn(
                                'whitespace-nowrap rounded-lg px-3 py-1.5 text-[11px] font-medium',
                                route.id === surface ? 'bg-pax-accent/15 text-pax-accent' : 'bg-white/5 text-pax-muted',
                            )}
                        >
                            {route.label}
                        </Link>
                    ))}
                </nav>
                <SurfaceView surface={surface} sidRate={sidRate} />
            </main>
        </WalletProvider>
    );
}
