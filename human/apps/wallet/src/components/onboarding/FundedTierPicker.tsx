'use client';

/**
 * Funded tier picker — the step that turns a freshly authenticated user
 * into a funded trader.
 *
 * Flow:
 *   1. List active tiers (already loaded by `EmbeddedWalletProvider`).
 *   2. Render one card per tier showing the economics — initial capital,
 *      drawdown caps, scale + payout thresholds, profit share.
 *   3. On select, call `provisionFunded(tier_id)`. The server disburses
 *      USDL + PAX from the treasury and returns once the on-chain
 *      transfers settle (~4-6s on chain 125), so we show a deterministic
 *      "Funding your account…" state during that window.
 *   4. On success, the `EmbeddedWalletProvider`'s `fundedSelf` flips
 *      non-null, which propagates through `refreshFunded` and lifts the
 *      shell out of onboarding.
 *
 * Used by `Onboarding.tsx` after the Funded sign-in flow completes.
 */

import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useEmbeddedWallet, type FundedTier } from '@/lib/wallet';
import { SvgIcon } from '@/components/ui/SvgIcon';

interface FundedTierPickerProps {
    onBack: () => void;
}

const ACCENT_FILTER =
    'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)';

export function FundedTierPicker({ onBack }: FundedTierPickerProps) {
    const embedded = useEmbeddedWallet();
    const { fundedTiers, fundedBusy, provisionFunded } = embedded;
    const [selectedTier, setSelectedTier] = useState<string | null>(null);
    const [error, setError] = useState<string | null>(null);

    const handleSelect = async (tier: FundedTier) => {
        setError(null);
        setSelectedTier(tier.tier_id);
        try {
            await provisionFunded(tier.tier_id);
            // Provider refreshes funded state; shell will route away
            // once `fundedSelf` flips non-null.
        } catch (err) {
            setSelectedTier(null);
            setError((err as Error).message || 'Failed to provision funded account');
        }
    };

    return (
        <div className="min-h-screen flex flex-col px-6 py-8 safe-area-pt">
            <button
                onClick={onBack}
                disabled={fundedBusy}
                className="self-start flex items-center gap-1.5 text-xs text-pax-muted press-scale disabled:opacity-40"
            >
                <SvgIcon
                    name="arrow-left"
                    className="w-3.5 h-3.5"
                    style={{ filter: 'brightness(0) invert(0.6)' }}
                />
                Back
            </button>

            <div className="flex flex-col items-center gap-2 mt-6 mb-6 text-center">
                <div className="w-16 h-16 rounded-3xl bg-pax-accent/10 flex items-center justify-center">
                    <SvgIcon
                        name="bridge"
                        className="w-9 h-9"
                        style={{ filter: ACCENT_FILTER }}
                    />
                </div>
                <h1 className="text-2xl font-bold tracking-tight">Pick your tier</h1>
                <p className="text-sm text-pax-muted max-w-sm">
                    Trade with funded capital. Profit share applies — no deposit, no withdrawal.
                </p>
            </div>

            {fundedTiers.length === 0 && !fundedBusy && (
                <div className="flex-1 flex items-center justify-center">
                    <p className="text-sm text-pax-muted">Loading tiers…</p>
                </div>
            )}

            <div className="flex flex-col gap-3">
                {fundedTiers.map((tier) => {
                    const busy = fundedBusy && selectedTier === tier.tier_id;
                    const disabled = fundedBusy;
                    const initialUsdl = formatUnits(tier.initial_usdl_units, tier.initial_usdl_decimals);
                    const scaleUsd = formatUnits(tier.scale_threshold_usd, 6);
                    const payoutUsd = formatUnits(tier.payout_threshold_usd, 6);

                    return (
                        <button
                            key={tier.tier_id}
                            onClick={() => handleSelect(tier)}
                            disabled={disabled}
                            className={
                                'w-full text-left bg-pax-surface rounded-2xl px-4 py-4 press-scale transition-all  ' +
                                (busy
                                    ? 'bg-pax-accent/10'
                                    : disabled
                                        ? 'opacity-40 cursor-not-allowed '
                                        : 'hover:bg-white/[0.07] ')
                            }
                        >
                            <div className="flex items-start justify-between gap-3">
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="text-base font-bold">{tier.label}</span>
                                        <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-pax-accent/15 text-pax-accent font-semibold">
                                            ${initialUsdl}
                                        </span>
                                    </div>
                                    <p className="text-[11px] text-pax-muted mt-1 leading-snug">
                                        Initial capital · {initialUsdl} USDL + gas grant
                                    </p>
                                </div>
                                {busy && <Loader2 className="w-4 h-4 animate-spin text-pax-accent shrink-0" />}
                            </div>

                            <div className="mt-3 grid grid-cols-2 gap-2 text-[11px]">
                                <Stat label="Daily DD" value={`${(tier.max_daily_dd_bps / 100).toFixed(1)}%`} />
                                <Stat label="Max DD" value={`${(tier.max_total_dd_bps / 100).toFixed(1)}%`} />
                                <Stat label="Scale at" value={`$${scaleUsd}`} />
                                <Stat label="Payout at" value={`$${payoutUsd}`} />
                            </div>

                            <div className="mt-3 pt-3   flex items-center justify-between text-[11px]">
                                <span className="text-pax-muted">Profit share</span>
                                <span className="font-semibold">
                                    {(tier.capital_fee_bps / 100).toFixed(1)}%
                                </span>
                            </div>
                        </button>
                    );
                })}
            </div>

            {fundedBusy && selectedTier && (
                <div className="mt-6 rounded-xl   bg-pax-accent/[0.06] px-4 py-3 flex items-center gap-3">
                    <Loader2 className="w-4 h-4 animate-spin text-pax-accent shrink-0" />
                    <div className="text-[11px] leading-snug">
                        <p className="font-semibold text-white">Funding your account…</p>
                        <p className="text-pax-muted">Disbursing USDL + PAX from treasury. Takes ~5s.</p>
                    </div>
                </div>
            )}

            {error && (
                <p className="mt-4 text-red-400 text-xs text-center">{error}</p>
            )}
        </div>
    );
}

function Stat({ label, value }: { label: string; value: string }) {
    return (
        <div className="flex items-center justify-between">
            <span className="text-pax-muted">{label}</span>
            <span className="font-semibold">{value}</span>
        </div>
    );
}

/**
 * Format an integer base-unit string to a human number, dropping trailing
 * zeros after the decimal point.
 *
 * `formatUnits("25000000000", 6) === "25,000"`
 */
function formatUnits(raw: string, decimals: number): string {
    if (!raw) return '0';
    const negative = raw.startsWith('-');
    const abs = negative ? raw.slice(1) : raw;
    const padded = abs.padStart(decimals + 1, '0');
    const whole = padded.slice(0, padded.length - decimals).replace(/^0+(?=\d)/, '');
    const frac = padded.slice(padded.length - decimals).replace(/0+$/, '');
    const withSeparators = whole.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
    const value = frac ? `${withSeparators}.${frac}` : withSeparators;
    return negative ? `-${value}` : value;
}
