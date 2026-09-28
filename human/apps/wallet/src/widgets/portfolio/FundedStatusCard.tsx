'use client';

/**
 * Funded status card — replaces the standard "hero balance" card in
 * Funded mode and surfaces what a funded trader actually cares about:
 *
 *   - Current equity (live, 10s poll, USD 6dp from server)
 *   - Peak equity + headroom bars for daily and max drawdown
 *   - Status pill: active · scale_eligible · payout_eligible · breached_* · closed
 *   - Tier label
 *   - Contextual CTAs:
 *       - "Take Payout" when status === 'payout_eligible'
 *       - Breach banner when status starts with 'breached_'
 *
 * Reads from `useEmbeddedWallet()` which is kept warm by
 * `EmbeddedWalletProvider`. We bump the refresh tick every 10s while
 * mounted so the evaluator's latest tick is reflected in the UI without
 * a manual reload.
 */

import { useEffect } from 'react';
import { AlertTriangle, TrendingUp, CheckCircle2 } from 'lucide-react';
import {
    useEmbeddedWallet,
    type FundedAccountStatus,
    type FundedSelfResponse,
} from '@/lib/wallet';
import { formatUsd } from '@/lib/format';

const POLL_INTERVAL_MS = 10_000;

export function FundedStatusCard() {
    const { fundedSelf, refreshFunded } = useEmbeddedWallet();

    // Live tick — the evaluator runs every 10s server-side, so we mirror
    // that cadence here. Keeps current equity / drawdown headroom fresh
    // without burning the API.
    useEffect(() => {
        const id = setInterval(refreshFunded, POLL_INTERVAL_MS);
        return () => clearInterval(id);
    }, [refreshFunded]);

    if (!fundedSelf) {
        return (
            <div className="col-span-2 bg-pax-surface rounded-[20px] p-4">
                <p className="text-sm text-pax-muted">Loading funded account…</p>
            </div>
        );
    }

    return <FundedStatusCardBody self={fundedSelf} />;
}

function FundedStatusCardBody({ self }: { self: FundedSelfResponse }) {
    const { funded_account: account, tier } = self;
    const status = account.status;

    const currentUsd = parseUsd6(account.current_value_usd);
    const peakUsd = parseUsd6(account.peak_value_usd);
    const startingUsd = parseUsd6(account.starting_value_usd);
    const dailyStartUsd = parseUsd6(account.daily_start_value_usd);

    // PnL since provisioning.
    const lifetimePnl = currentUsd - startingUsd;
    const lifetimePnlPct = startingUsd > 0 ? (lifetimePnl / startingUsd) * 100 : 0;

    // Drawdown headroom: current drawdown is (peak - current) / peak,
    // headroom is (cap - current_dd) / cap. We display as a % of cap
    // consumed (so the bar fills up as the trader approaches breach).
    const dailyCapBps = tier?.max_daily_dd_bps ?? 0;
    const totalCapBps = tier?.max_total_dd_bps ?? 0;

    const dailyDdBps =
        dailyStartUsd > 0
            ? Math.max(0, ((dailyStartUsd - currentUsd) / dailyStartUsd) * 10_000)
            : 0;
    const totalDdBps =
        peakUsd > 0
            ? Math.max(0, ((peakUsd - currentUsd) / peakUsd) * 10_000)
            : 0;

    const dailyConsumed = dailyCapBps > 0 ? Math.min(100, (dailyDdBps / dailyCapBps) * 100) : 0;
    const totalConsumed = totalCapBps > 0 ? Math.min(100, (totalDdBps / totalCapBps) * 100) : 0;

    return (
        <div className="col-span-2 bg-pax-surface rounded-[20px] p-4 flex flex-col gap-3">
            {/* ── Header: tier + status pill ─────────────────────────────── */}
            <div className="flex items-center justify-between gap-2">
                <div className="flex flex-col">
                    <span className="text-[10px] uppercase tracking-wider text-pax-muted">
                        Funded · {tier?.label ?? 'Unknown tier'}
                    </span>
                    <span className="text-xs text-pax-muted mt-0.5">
                        {formatUsd(currentUsd)} · current equity
                    </span>
                </div>
                <StatusPill status={status} />
            </div>

            {/* ── Hero metric: PnL since provisioning ───────────────────── */}
            <div className="flex items-end justify-between gap-2 mt-1">
                <div className="flex flex-col">
                    <span className={`text-2xl font-bold tracking-tight ${lifetimePnl >= 0 ? 'text-pax-success' : 'text-pax-error'}`}>
                        {lifetimePnl >= 0 ? '+' : ''}{formatUsd(Math.abs(lifetimePnl))}
                    </span>
                    <span className="text-[11px] text-pax-muted mt-0.5">
                        {lifetimePnl >= 0 ? '+' : '-'}{Math.abs(lifetimePnlPct).toFixed(2)}% since funding
                    </span>
                </div>
                <div className="flex flex-col items-end text-right">
                    <span className="text-[10px] uppercase tracking-wider text-pax-muted">Peak</span>
                    <span className="text-sm font-semibold">{formatUsd(peakUsd)}</span>
                </div>
            </div>

            {/* ── Drawdown bars ──────────────────────────────────────────── */}
            <div className="grid grid-cols-2 gap-3 mt-1">
                <DdBar
                    label="Daily DD"
                    consumed={dailyConsumed}
                    capLabel={`${(dailyCapBps / 100).toFixed(1)}%`}
                />
                <DdBar
                    label="Max DD"
                    consumed={totalConsumed}
                    capLabel={`${(totalCapBps / 100).toFixed(1)}%`}
                />
            </div>

            {/* ── Contextual banners ─────────────────────────────────────── */}
            {status === 'payout_eligible' && (
                <div className="mt-1 rounded-xl   bg-pax-success/[0.08] px-3 py-2.5 flex items-center gap-2.5">
                    <CheckCircle2 className="w-4 h-4 text-pax-success shrink-0" />
                    <div className="flex-1 min-w-0 text-[11px] leading-snug">
                        <p className="font-semibold text-white">Payout eligible</p>
                        <p className="text-pax-muted">
                            You hit the payout threshold. Profit share: {tier ? (tier.capital_fee_bps / 100).toFixed(1) : '—'}%.
                        </p>
                    </div>
                </div>
            )}

            {status === 'scale_eligible' && (
                <div className="mt-1 rounded-xl   bg-pax-accent/[0.06] px-3 py-2.5 flex items-center gap-2.5">
                    <TrendingUp className="w-4 h-4 text-pax-accent shrink-0" />
                    <div className="flex-1 min-w-0 text-[11px] leading-snug">
                        <p className="font-semibold text-white">Scale eligible</p>
                        <p className="text-pax-muted">
                            Hit the scale threshold — your tier can be upgraded.
                        </p>
                    </div>
                </div>
            )}

            {(status === 'breached_daily' || status === 'breached_max' || status === 'closed') && (
                <div className="mt-1 rounded-xl   bg-pax-error/[0.08] px-3 py-2.5 flex items-center gap-2.5">
                    <AlertTriangle className="w-4 h-4 text-pax-error shrink-0" />
                    <div className="flex-1 min-w-0 text-[11px] leading-snug">
                        <p className="font-semibold text-white">
                            {status === 'breached_daily'
                                ? 'Daily drawdown breached'
                                : status === 'breached_max'
                                    ? 'Max drawdown breached'
                                    : 'Account closed'}
                        </p>
                        <p className="text-pax-muted">
                            {account.breached_reason ?? 'Trading paused. Open a new tier from Settings to resume.'}
                        </p>
                    </div>
                </div>
            )}
        </div>
    );
}

function StatusPill({ status }: { status: FundedAccountStatus }) {
    const meta = STATUS_META[status];
    return (
        <span
            className={`text-[10px] uppercase tracking-wider px-2 py-0.5 rounded-md font-semibold ${meta.className}`}
        >
            {meta.label}
        </span>
    );
}

function DdBar({ label, consumed, capLabel }: { label: string; consumed: number; capLabel: string }) {
    // Color shifts as the bar fills: green → amber → red.
    const color =
        consumed >= 80 ? 'bg-pax-error' :
        consumed >= 50 ? 'bg-pax-warning' :
        'bg-pax-success';
    return (
        <div className="flex flex-col gap-1">
            <div className="flex items-center justify-between text-[11px]">
                <span className="text-pax-muted">{label}</span>
                <span className="font-semibold">
                    {consumed.toFixed(0)}% <span className="text-pax-muted font-normal">of {capLabel}</span>
                </span>
            </div>
            <div className="h-1.5 rounded-full bg-white/[0.06] overflow-hidden">
                <div
                    className={`h-full ${color} transition-all`}
                    style={{ width: `${Math.max(2, consumed)}%` }}
                />
            </div>
        </div>
    );
}

// ── Helpers ───────────────────────────────────────────────────────────

const STATUS_META: Record<FundedAccountStatus, { label: string; className: string }> = {
    pending_funding: { label: 'Pending', className: 'bg-white/[0.06] text-pax-muted' },
    active: { label: 'Active', className: 'bg-pax-success/15 text-pax-success' },
    scale_eligible: { label: 'Scale eligible', className: 'bg-pax-accent/15 text-pax-accent' },
    payout_eligible: { label: 'Payout', className: 'bg-pax-success/15 text-pax-success' },
    breached_daily: { label: 'Breached daily', className: 'bg-pax-error/15 text-pax-error' },
    breached_max: { label: 'Breached max', className: 'bg-pax-error/15 text-pax-error' },
    closed: { label: 'Closed', className: 'bg-white/[0.06] text-pax-muted' },
};

/** Parse a USD value stored as a 6dp string ("12345000000" → 12_345.00). */
function parseUsd6(raw: string | null | undefined): number {
    if (!raw) return 0;
    // Avoid bigint precision loss for the integer part, then re-attach
    // fractional component.
    const negative = raw.startsWith('-');
    const abs = negative ? raw.slice(1) : raw;
    const padded = abs.padStart(7, '0');
    const whole = padded.slice(0, padded.length - 6);
    const frac = padded.slice(padded.length - 6);
    const n = Number(whole) + Number(frac) / 1_000_000;
    return negative ? -n : n;
}
