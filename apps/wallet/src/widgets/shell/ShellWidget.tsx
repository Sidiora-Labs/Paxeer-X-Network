'use client';

import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { useWallet } from '@/wallet/WalletProvider';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { Onboarding } from '@/components/onboarding/Onboarding';
import { PortfolioWidget } from '@/widgets/portfolio';
import { SendWidget } from '@/widgets/send';
import { ReceiveWidget } from '@/widgets/receive';
import { TransactionsWidget } from '@/widgets/transactions';
import { SwapWidget } from '@/widgets/swap';
import { SettingsWidget } from '@/widgets/settings';
import { TokenDetailWidget } from '@/widgets/tokenDetail';
import { TxDetailWidget } from '@/widgets/txDetail';
import { DiscoverWidget } from '@/widgets/discover';
import { ContactsWidget } from '@/widgets/contacts';
import { ErrorBoundary } from '@/components/ui/ErrorBoundary';
import { PNSWidget } from '@/widgets/pns';
import { BottomNav } from '@/components/nav/BottomNav';
import { UniversalHeader } from '@/components/nav/UniversalHeader';
import { WhatsNewModal } from '@/components/ui/WhatsNewModal';
import { useAppRoute, type AppRoute } from './useAppRoute';
import { useNotificationLifecycle } from './useNotificationLifecycle';
import { openExternalUrl, openExplorerPath } from '@/lib/security/navigation';
import { BackgroundStatus } from '@/components/ui/BackgroundStatus';
import { Loader2 } from 'lucide-react';
import { routeGuard, type ShellRoute } from '@/domains/shell';
import { pendingSendRepository } from '@/platform/storage/repositories';

function openExternal(url: string) {
    openExternalUrl(url);
}

function unreachableRoute(route: never): never {
    throw new Error(`Unreachable route: ${String(route)}`);
}

export function ShellWidget() {
    const { ready, activeAccount } = useWalletState();
    const { status, mode } = useWallet();
    const hydrated = status !== 'loading';
    useNotificationLifecycle();

    const appRoute = useAppRoute();
    const {
        routeState,
        route: requestedRoute,
        setRoute,
        replaceRoute,
        goBack,
        tokenDetailId,
        tokenDetailSymbol,
        txDetailHash,
        sendTokenId,
        navigateToSend,
        navigateToToken,
        navigateToTx,
    } = appRoute;
    const shellEligible = status === 'ready' && mode !== null;
    const routeDecision = useMemo(
        () =>
            !shellEligible || mode === null
                ? { allowed: true as const }
                : routeGuard(routeState, {
                    custody: mode,
                    unlocked: status === 'ready',
                    hasAccount: Boolean(activeAccount),
                }),
        [
            activeAccount,
            mode,
            status,
            routeState,
            shellEligible,
        ],
    );
    const route = routeDecision.allowed
        ? requestedRoute
        : routeDecision.recovery.name;

    useEffect(() => {
        if (!hydrated || !ready || routeDecision.allowed) return;
        replaceRoute(routeDecision.recovery);
    }, [hydrated, ready, replaceRoute, routeDecision]);

    const previousAccount = useRef<string | null>(null);
    useEffect(() => {
        const current = activeAccount?.address ?? null;
        if (previousAccount.current !== null && previousAccount.current !== current) {
            pendingSendRepository.remove();
        }
        previousAccount.current = current;
    }, [activeAccount?.address]);

    const backTo = useCallback(
        (fallback: ShellRoute) => goBack(fallback),
        [goBack],
    );

    const setRouteSafe = useCallback(
        (next: AppRoute) => {
            setRoute(next);
        },
        [setRoute],
    );

    const navigateToTrade = useCallback((poolAddress: string) => {
        openExternal(`https://www.kindlelaunch.com/token/${poolAddress}`);
    }, []);

    const navigateToPaxscan = useCallback((path?: string) => {
        openExplorerPath(path);
    }, []);

    // Header config per route — must be a hook (useMemo) so it stays above early returns
    const headerConfig = useMemo(() => {
        switch (route) {
            case 'portfolio': return { title: 'Wallet' };
            case 'transactions': return { title: 'Activity' };
            case 'swap': return { title: 'Swap' };
            case 'discover': return { title: 'Discover' };
            case 'settings': return { title: 'Settings' };
            case 'token-detail': return {
                title: tokenDetailSymbol || 'Token',
                showBack: true,
                onBack: () => backTo({ name: 'portfolio' }),
                rightAction: tokenDetailId && tokenDetailId !== 'pax' ? (
                    <button
                        onClick={() => navigateToPaxscan(`/token/${tokenDetailId}`)}
                        className="p-2 press-scale"
                    >
                        <SvgIcon name="external-link" className="w-4.5 h-4.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                    </button>
                ) : undefined,
            };
            case 'tx-detail': return {
                title: 'Transaction',
                showBack: true,
                onBack: () => backTo({ name: 'transactions' }),
            };
            default: return { title: '' };
        }
    }, [route, tokenDetailSymbol, tokenDetailId, navigateToPaxscan, backTo]);

    // ── Loading / hydrate ────────────────────────────────────────────────
    if (!hydrated || !ready) {
        return (
            <div className="min-h-screen flex items-center justify-center">
                <div className="flex flex-col items-center gap-3">
                    <Loader2
                        aria-label="Loading wallet"
                        className="h-10 w-10 animate-spin text-pax-accent"
                    />
                    <p className="text-sm text-pax-muted">Loading wallet…</p>
                </div>
            </div>
        );
    }

    if (status === 'connecting') {
        return <Onboarding initialStep="embedded-setup" />;
    }

    if (status !== 'ready' || mode === null) {
        return <Onboarding />;
    }

    const showNav = !['send', 'receive', 'contacts', 'pns'].includes(route);
    const showHeader = showNav;

    // Main app
    const renderPage = () => {
        switch (route) {
            case 'portfolio':
                return (
                    <PortfolioWidget
                        onNavigate={setRouteSafe}
                        onTokenDetail={navigateToToken}
                    />
                );
            case 'send':
                return <SendWidget onBack={() => backTo({ name: 'portfolio' })} preSelectTokenAddress={sendTokenId || undefined} onPaxscan={navigateToPaxscan} />;
            case 'receive':
                return <ReceiveWidget onBack={() => backTo({ name: 'portfolio' })} />;
            case 'transactions':
                return <TransactionsWidget onTxDetail={navigateToTx} />;
            case 'swap':
                return <SwapWidget onPaxscan={navigateToPaxscan} />;
            case 'settings':
                return <SettingsWidget onNavigate={setRouteSafe} onPaxscan={navigateToPaxscan} />;
            case 'contacts':
                return <ContactsWidget onBack={() => backTo({ name: 'settings' })} />;
            case 'discover':
                return <DiscoverWidget onNavigate={setRouteSafe} onTokenTrade={navigateToTrade} />;
            case 'token-detail':
                return (
                    <TokenDetailWidget
                        tokenId={tokenDetailId}
                        onNavigate={setRouteSafe}
                        onTxDetail={navigateToTx}
                        onSendToken={navigateToSend}
                    />
                );
            case 'tx-detail':
                return <TxDetailWidget txHash={txDetailHash} onBack={() => backTo({ name: 'transactions' })} onPaxscan={navigateToPaxscan} />;
            case 'pns':
                return <PNSWidget onBack={() => backTo({ name: 'discover' })} onPaxscan={navigateToPaxscan} />;
            default:
                return unreachableRoute(route);
        }
    };

    return (
        <div className={`min-h-screen ${showNav ? 'pb-20' : ''}`}>
            <WhatsNewModal />
            <BackgroundStatus />
            {showHeader && (
                <>
                    <UniversalHeader {...headerConfig} />
                    {/* Spacer matching fixed header height: safe-area-top + h-14 content */}
                    <div className="shrink-0 safe-area-pt">
                        <div className="h-14" />
                    </div>
                </>
            )}
            <ErrorBoundary key={route}>
                <div className="animate-fade-in">{renderPage()}</div>
            </ErrorBoundary>
            {showNav && (
                <BottomNav
                    active={
                        route === 'token-detail' ? 'portfolio'
                            : route === 'tx-detail' ? 'transactions'
                                : route
                    }
                    onNavigate={setRouteSafe}
                />
            )}
        </div>
    );
}

// Re-export for consumers that reference AppRoute from this widget
export type { AppRoute } from './useAppRoute';
