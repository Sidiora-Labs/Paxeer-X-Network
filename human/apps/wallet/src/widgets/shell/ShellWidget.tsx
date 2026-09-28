'use client';

import { useCallback, useEffect, useMemo, useRef } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { useOptionalEmbeddedWallet } from '@/lib/wallet';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { Onboarding } from '@/components/onboarding/Onboarding';
import { PinLock } from '@/components/auth/PinLock';
import { PortfolioWidget } from '@/widgets/portfolio';
import { SendWidget } from '@/widgets/send';
import { ReceiveWidget } from '@/widgets/receive';
import { TransactionsWidget } from '@/widgets/transactions';
import { SwapWidget } from '@/widgets/swap';
import { SettingsWidget } from '@/widgets/settings';
import { TokenDetailWidget } from '@/widgets/tokenDetail';
import { TxDetailWidget } from '@/widgets/txDetail';
import { DAppBrowserWidget } from '@/widgets/dappBrowser';
import { DiscoverWidget } from '@/widgets/discover';
import { ContactsWidget } from '@/widgets/contacts';
import { RampWidget } from '@/widgets/ramp';
import { ErrorBoundary } from '@/components/ui/ErrorBoundary';
import { PNSWidget } from '@/widgets/pns';
import { BottomNav } from '@/components/nav/BottomNav';
import { UniversalHeader } from '@/components/nav/UniversalHeader';
import { WhatsNewModal } from '@/components/ui/WhatsNewModal';
import { useAppRoute, type AppRoute } from './useAppRoute';
import { useNotificationLifecycle } from './useNotificationLifecycle';
import { openExternalUrl } from '@/lib/security/navigation';
import { BackgroundStatus } from '@/components/ui/BackgroundStatus';
import { Loader2 } from 'lucide-react';
import { routeGuard, type ShellRoute } from '@/domains/shell';
import { pendingSendRepository } from '@/platform/storage/repositories';

// ── External DApp URL map ────────────────────────────────────────────
//
// In **embedded** mode the in-app DApp browser is replaced with direct
// redirects — embedded users sign into the standalone apps with the same
// Paxeer Wallet identity, no postMessage bridge needed. We list the
// canonical URLs here so the shell can route taps from `DiscoverWidget`
// straight to `window.open(...)` without ever mounting the iframe.
const EXTERNAL_DAPP_URLS: Partial<Record<AppRoute, string>> = {
    dex: 'https://app.hyperpax.xyz',
    colosseum: 'https://colosseum.hyperpaxeer.com',
    dao: 'https://dao.hyperpaxeer.com',
    wormhole: 'https://crossverse.app',
    points: 'https://app.webpoints.app',
    paxscan: 'https://paxscan.io',
    'sidiora-fun': 'https://www.kindlelaunch.com',
};

const DAPP_ROUTES: ReadonlySet<AppRoute> = new Set<AppRoute>([
    'dex',
    'colosseum',
    'dao',
    'paxfun',
    'wormhole',
    'points',
    'paxscan',
    'pns',
    'sidiora-fun',
    'browser',
]);

// Routes that are structurally disabled in Funded mode. The Funded
// policy engine blocks withdrawals and contract interactions outside
// the tier whitelist server-side; gating the UI here keeps users from
// landing on screens that would offer those actions and then fail at
// the signing layer.
const FUNDED_BANNED_ROUTES: ReadonlySet<AppRoute> = new Set<AppRoute>([
    'send',
    'receive',
    'ramp',
    'contacts',
]);

function openExternal(url: string) {
    openExternalUrl(url);
}

function unreachableRoute(route: never): never {
    throw new Error(`Unreachable route: ${String(route)}`);
}

export function ShellWidget() {
    const { ready, hasWallet, isLocked, activeAccount } = useWalletState();
    const { kind, hydrated } = useWalletKind();
    const embedded = useOptionalEmbeddedWallet();
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
        paxfunUrl,
        paxfunTitle,
        browserUrl,
        browserTitle,
        sendTokenId,
        navigateToSend,
        navigateToToken,
        navigateToTx,
        navigateToTrade: rawNavigateToTrade,
        navigateToBrowser: rawNavigateToBrowser,
        navigateToPaxscan: rawNavigateToPaxscan,
    } = appRoute;
    const enabledFeatures = useMemo(() => new Set(['dapp-browser']), []);
    const shellEligible =
        kind === 'self-custody'
            ? hasWallet && !isLocked && Boolean(activeAccount)
            : kind === 'embedded'
                ? Boolean(embedded?.isAuthenticated && embedded.publicWallet)
                : kind === 'funded'
                    ? Boolean(embedded?.isAuthenticated && embedded.fundedSelf)
                    : false;
    const routeDecision = useMemo(
        () =>
            !shellEligible || kind === null
                ? { allowed: true as const }
                : routeGuard(routeState, {
                    custody: kind,
                    unlocked:
                        kind === 'self-custody'
                            ? !isLocked
                            : Boolean(embedded?.isAuthenticated),
                    hasAccount: Boolean(activeAccount),
                    features: enabledFeatures,
                }),
        [
            activeAccount,
            embedded?.isAuthenticated,
            enabledFeatures,
            isLocked,
            kind,
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
        if (
            isLocked ||
            (previousAccount.current !== null && previousAccount.current !== current)
        ) {
            pendingSendRepository.remove();
        }
        previousAccount.current = current;
    }, [activeAccount?.address, isLocked]);

    const backTo = useCallback(
        (fallback: ShellRoute) => goBack(fallback),
        [goBack],
    );

    // ── Embedded-mode redirect interceptors ──────────────────────────────
    // These wrap the `setRoute` and DApp-navigation helpers so DApp tiles
    // open in a new tab when the user is signed in with the embedded wallet,
    // and behave normally for self-custody.
    const isEmbedded = kind === 'embedded';
    const isFunded = kind === 'funded';

    const setRouteSafe = useCallback(
        (next: AppRoute) => {
            // Funded users can't reach send / receive / ramp / contacts —
            // bounce them back to the portfolio if anything tries to
            // navigate there. (ActionBento and TokenActions hide the
            // entry points already; this is defence-in-depth.)
            if (isFunded && FUNDED_BANNED_ROUTES.has(next)) {
                setRoute('portfolio');
                return;
            }
            if ((isEmbedded || isFunded) && DAPP_ROUTES.has(next)) {
                const url = EXTERNAL_DAPP_URLS[next];
                if (url) openExternal(url);
                // Don't change in-app route — keep the user on Discover.
                return;
            }
            setRoute(next);
        },
        [isEmbedded, isFunded, setRoute],
    );

    const navigateToTrade = useCallback(
        (poolAddress: string, symbol?: string) => {
            if (isEmbedded) {
                openExternal(`https://www.kindlelaunch.com/token/${poolAddress}`);
                return;
            }
            rawNavigateToTrade(poolAddress, symbol);
        },
        [isEmbedded, rawNavigateToTrade],
    );

    const navigateToBrowser = useCallback(
        (url: string) => {
            if (isEmbedded) {
                try {
                    const u = new URL(url.startsWith('http') ? url : `https://${url}`);
                    openExternal(u.toString());
                } catch {
                    /* invalid URL — silently ignore */
                }
                return;
            }
            rawNavigateToBrowser(url);
        },
        [isEmbedded, rawNavigateToBrowser],
    );

    const navigateToPaxscan = useCallback(
        (path?: string) => {
            if (isEmbedded) {
                const base = 'https://paxscan.io';
                openExternal(path ? `${base}${path}` : base);
                return;
            }
            rawNavigateToPaxscan(path);
        },
        [isEmbedded, rawNavigateToPaxscan],
    );

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

    // ── Onboarding (no kind chosen yet) ──────────────────────────────────
    if (kind === null) {
        return <Onboarding />;
    }

    // ── Embedded mode gating ─────────────────────────────────────────────
    if (kind === 'embedded') {
        if (!embedded?.isAuthenticated) {
            // Land users straight in the embedded sign-in screen — they already
            // picked the embedded card on a previous visit, so the welcome
            // screen would just be a wasted tap.
            return <Onboarding initialStep="embedded-signin" />;
        }
        if (!embedded.publicWallet) {
            // Authenticated but standard wallet not yet provisioned. Route
            // back through onboarding's `embedded-setup` step which kicks
            // the explicit `provisionStandard()` call and shows a
            // deterministic loading screen until `publicWallet` flips
            // non-null.
            return <Onboarding initialStep="embedded-setup" />;
        }
        // Authenticated + provisioned — fall through to render the shell.
    }

    // ── Funded mode gating ───────────────────────────────────────────────
    //
    // Mirrors the embedded gates but pivots on `fundedSelf` instead of
    // `publicWallet`. When the user picks the Funded tile we set
    // `kind='funded'` synchronously, which is what gets us here. From
    // here the shell drives the rest:
    //
    //   1. No session → bounce to the funded sign-in screen so the user
    //      can authenticate with email / OAuth.
    //   2. Session but no funded account → tier picker. The picker
    //      calls `provisionFunded(tier_id)` which disburses USDL + PAX
    //      and flips `fundedSelf` non-null.
    //   3. Authenticated + provisioned → fall through to render the shell
    //      with the funded portfolio (`FundedStatusCard` + Swap/Trade
    //      bento + filtered holdings).
    if (kind === 'funded') {
        if (!embedded?.isAuthenticated) {
            return <Onboarding initialStep="funded-signin" />;
        }
        if (!embedded.fundedSelf) {
            return <Onboarding initialStep="funded-tier-picker" />;
        }
        // Authenticated + provisioned — fall through to render the shell.
    }

    // ── Self-custody mode gating (unchanged behavior) ────────────────────
    if (kind === 'self-custody') {
        if (!hasWallet) {
            return <Onboarding />;
        }
        if (isLocked) {
            return <PinLock />;
        }
    }

    const showNav = !['send', 'receive', 'contacts', 'ramp', 'dex', 'colosseum', 'dao', 'paxfun', 'wormhole', 'points', 'paxscan', 'pns', 'sidiora-fun', 'browser'].includes(route);
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
            case 'ramp':
                return <RampWidget onBack={() => backTo({ name: 'portfolio' })} />;
            case 'discover':
                return <DiscoverWidget onNavigate={setRouteSafe} onTokenTrade={navigateToTrade} onBrowse={navigateToBrowser} />;
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
            case 'dex':
                return (
                    <DAppBrowserWidget
                        url="https://app.hyperpax.xyz"
                        title="Sidiora DEX"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'colosseum':
                return (
                    <DAppBrowserWidget
                        url="https://colosseum.hyperpaxeer.com"
                        title="PAX Colosseum"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'dao':
                return (
                    <DAppBrowserWidget
                        url="https://dao.hyperpaxeer.com"
                        title="Paxeer DAO"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'paxfun':
                return (
                    <DAppBrowserWidget
                        url={paxfunUrl}
                        title={paxfunTitle}
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'wormhole':
                return (
                    <DAppBrowserWidget
                        url="https://crossverse.app"
                        title="CrossVerse"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'points':
                return (
                    <DAppBrowserWidget
                        url="https://app.webpoints.app"
                        title="Paxeer Points"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'pns':
                return <PNSWidget onBack={() => backTo({ name: 'discover' })} onPaxscan={navigateToPaxscan} />;
            case 'sidiora-fun':
                return (
                    <DAppBrowserWidget
                        url="https://www.kindlelaunch.com"
                        title="Kindle Launch"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'paxscan':
                return (
                    <DAppBrowserWidget
                        url={browserUrl || 'https://paxscan.io'}
                        title="PaxScan"
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
            case 'browser':
                return (
                    <DAppBrowserWidget
                        url={browserUrl}
                        title={browserTitle}
                        onBack={() => backTo({ name: 'discover' })}
                        onNavigate={setRouteSafe}
                    />
                );
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
