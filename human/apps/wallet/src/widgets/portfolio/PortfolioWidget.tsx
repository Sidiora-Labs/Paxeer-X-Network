'use client';

/**
 * Portfolio widget — orchestrator for the wallet's main view.
 *
 * Composes:
 * - {@link HeroBalance}    — total USD + daily PnL + PAX spot
 * - {@link ActionBento}    — Send / Swap / Bridge / Receive / Buy
 * - {@link HoldingsFilter} — dust + per-token visibility controls
 * - {@link HoldingsGrid}   — bento layout of native + tokens
 *
 * Owns:
 * - data fetching (portfolio, balance, PAX price)
 * - filter state (via {@link useTokenFilters})
 * - optimistic guard ref (the post-send cache mutation effect lives in
 *   {@link useApplyPendingSend} and writes back into the same ref)
 *
 * Replaces the legacy `PortfolioPage` component. Same prop contract so
 * `WalletShell` can swap call sites without changes.
 */

import { useCallback, useRef } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { formatBalance } from '@/lib/format';
import { ErrorBanner } from '@/components/ui/NetworkErrorScreen';
import { PortfolioSkeleton } from '@/components/ui/Skeletons';
import {
    usePortfolioQuery,
    useBalanceQuery,
    usePaxPriceQuery,
} from '@/lib/queries';
import type { AppRoute } from '@/widgets/shell/useAppRoute';
import { HeroBalance } from './HeroBalance';
import { ActionBento } from './ActionBento';
import { FundedStatusCard } from './FundedStatusCard';
import { HoldingsFilter } from './HoldingsFilter';
import { HoldingsGrid, type HoldingsGridHolding } from './HoldingsGrid';
import { PaxCardExpand } from './PaxCardExpand';
import { useTokenFilters } from './useTokenFilters';
import { useApplyPendingSend } from './useApplyPendingSend';

export interface PortfolioWidgetProps {
    onNavigate: (route: AppRoute) => void;
    onTokenDetail?: (tokenId: string, symbol?: string) => void;
}

export function PortfolioWidget({ onNavigate, onTokenDetail }: PortfolioWidgetProps) {
    const { activeAccount } = useWalletState();
    const { kind } = useWalletKind();
    const address = activeAccount?.address;
    const isFunded = kind === 'funded';

    // ── Optimistic guard ─────────────────────────────────────────────────
    // While a post-send mutation is in flight, query hooks must skip background
    // refetches so the optimistic delta isn't stomped by a stale read. The ref
    // is mutated by `useApplyPendingSend` once the pending send has been
    // applied to the cache.
    const optimisticBlockUntilRef = useRef(0);
    const isOptimisticActive = useCallback(
        () => Date.now() < optimisticBlockUntilRef.current,
        [],
    );

    // ── Queries ──────────────────────────────────────────────────────────
    const portfolioQuery = usePortfolioQuery(address, {
        refetchOnWindowFocus: () => !isOptimisticActive(),
        refetchOnReconnect: () => !isOptimisticActive(),
    });
    const balanceQuery = useBalanceQuery(address, {
        refetchOnWindowFocus: () => !isOptimisticActive(),
        refetchOnReconnect: () => !isOptimisticActive(),
    });
    const paxPriceQuery = usePaxPriceQuery();

    const portfolio = portfolioQuery.data ?? null;
    const balanceData = balanceQuery.data ?? null;
    const paxPrice = paxPriceQuery.data?.latest ?? 0;
    const loading = portfolioQuery.isPending || balanceQuery.isPending;
    const hasError = portfolioQuery.isError || balanceQuery.isError;

    // ── Optimistic effect (runs when cache is populated) ─────────────────
    useApplyPendingSend({ address, loading, optimisticBlockUntilRef });

    // ── Derived values ───────────────────────────────────────────────────
    const nativeBalanceRaw = portfolio?.native_balance?.balance_raw || '0';
    const nativeBalance = formatBalance(nativeBalanceRaw, 18, 4);
    const nativeValueUsd = Number(formatBalance(nativeBalanceRaw, 18, 8)) * paxPrice;

    const allHoldings: HoldingsGridHolding[] = portfolio?.token_holdings || [];
    const tokenValueUsd = allHoldings.reduce(
        (sum, h) => sum + (h.value_usd != null ? Number(h.value_usd) : 0),
        0,
    );
    const totalUsd = nativeValueUsd + tokenValueUsd;

    // useTokenFilters must be called unconditionally before any early return.
    const filters = useTokenFilters();
    const holdings = filters.applyFilters(allHoldings);

    // ── First-load skeleton ──────────────────────────────────────────────
    // All hooks are above this line — safe to early-return here.
    // `isPending` is true until the first successful fetch; after that TanStack
    // Query returns stale data immediately so this guard never fires again.
    if (loading && !portfolio && !balanceData) return <PortfolioSkeleton />;

    return (
        <div className="flex flex-col">
            {hasError && !loading && (
                <ErrorBanner
                    message="Could not refresh portfolio. Showing last cached data."
                    onRetry={() => { portfolioQuery.refetch(); balanceQuery.refetch(); }}
                />
            )}
            <div className="grid grid-cols-2 gap-2.5 px-3 pt-3 pb-4">
                {/*
                  Funded mode replaces the standard "hero balance + PAX
                  card" pair with a single status card that surfaces
                  equity, drawdown headroom, and tier state — the metrics
                  a funded trader actually cares about. Token holdings
                  (HoldingsGrid) still render below so users can see what's
                  in their funded wallet.
                */}
                {isFunded ? (
                    <FundedStatusCard />
                ) : (
                    <HeroBalance
                        totalUsd={totalUsd}
                        loading={loading}
                        paxPrice={paxPrice}
                        dailyPnlUsd={balanceData?.daily_pnl_usd ?? null}
                        dailyPnlPercent={balanceData?.daily_pnl_percent ?? null}
                    />
                )}

                <ActionBento onNavigate={onNavigate} />

                {!isFunded && (
                    <div className="col-span-2">
                        <PaxCardExpand address={address} onTokenDetail={onTokenDetail} />
                    </div>
                )}

                {allHoldings.length > 3 && (
                    <>
                        <HoldingsFilter
                            allHoldings={allHoldings}
                            hideDust={filters.hideDust}
                            hiddenTokens={filters.hiddenTokens}
                            onToggleHideDust={filters.toggleHideDust}
                            onToggleTokenVisibility={filters.toggleTokenVisibility}
                        />

                        <HoldingsGrid
                            hidden={false}
                            loading={loading}
                            nativeBalance={nativeBalance}
                            nativeValueUsd={nativeValueUsd}
                            holdings={holdings}
                            allHoldingsCount={allHoldings.length}
                            onTokenDetail={onTokenDetail}
                        />
                    </>
                )}
            </div>
        </div>
    );
}
