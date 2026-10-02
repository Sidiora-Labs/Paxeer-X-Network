'use client';

/**
 * Bento token cards — three layout variants used in the portfolio grid.
 *
 * - **Tall**:    full-height left column, used for the native PAX card
 * - **Compact**: standard 1×1 cell, used for most holdings
 * - **Wide**:    full-width row, used for the 3rd holding to break up the grid
 *
 * Pure presentational components. All formatting must be done by the caller.
 */

import Image from "next/image";
import { formatUsd } from '@/lib/format';

export interface BentoTokenCardProps {
    symbol: string;
    name: string;
    balance: string;
    valueUsd: number | null;
    iconUrl: string | null;
    loading?: boolean;
    onClick?: () => void;
}

export function BentoTokenTall({
    symbol,
    name,
    balance,
    valueUsd,
    iconUrl,
    loading,
    onClick,
}: BentoTokenCardProps) {
    return (
        <button
            onClick={onClick}
            className="row-span-2 bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[200px]"
        >
            <div className="flex flex-col gap-3">
                <div className="relative w-[42px] h-[42px] rounded-full bg-[var(--color-surface-control)] overflow-hidden shrink-0">
                    <Image src={iconUrl || '/wallet/default_icon.webp'} alt={symbol} fill sizes="40px" className="w-full h-full object-cover" />
                </div>
                <div>
                    <p className="text-base font-extrabold leading-tight">{name}</p>
                    <p className="text-[11px] text-pax-muted mt-0.5">{symbol}</p>
                </div>
            </div>
            <div className="mt-auto pt-3">
                {loading ? (
                    <div className="h-6 w-20 shimmer rounded" />
                ) : (
                    <>
                        <p className="text-[22px] font-extrabold tracking-[-0.02em] leading-none">{balance}</p>
                        {valueUsd !== null && valueUsd > 0 && (
                            <p className="text-xs text-pax-subtle mt-1">{formatUsd(valueUsd)}</p>
                        )}
                    </>
                )}
            </div>
        </button>
    );
}

export function BentoTokenCompact({
    symbol,
    name,
    balance,
    valueUsd,
    iconUrl,
    onClick,
}: BentoTokenCardProps) {
    return (
        <button
            onClick={onClick}
            className="bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[110px]"
        >
            <div className="flex items-center gap-2.5">
                <div className="relative w-[30px] h-[30px] rounded-full bg-[var(--color-surface-control)] overflow-hidden shrink-0">
                    <Image src={iconUrl || '/wallet/default_icon.webp'} alt={symbol} fill sizes="40px" className="w-full h-full object-cover" />
                </div>
                <div>
                    <p className="text-[13px] font-bold">{symbol}</p>
                    <p className="text-[10px] text-pax-muted">{name}</p>
                </div>
            </div>
            <div className="mt-auto pt-2">
                <p className="text-[15px] font-bold">{balance}</p>
                {valueUsd !== null && valueUsd > 0 && (
                    <p className="text-[10px] text-pax-muted mt-0.5">{formatUsd(valueUsd)}</p>
                )}
            </div>
        </button>
    );
}

export function BentoTokenWide({
    symbol,
    name,
    balance,
    valueUsd,
    iconUrl,
    onClick,
}: BentoTokenCardProps) {
    return (
        <button
            onClick={onClick}
            className="col-span-2 bg-pax-surface rounded-[20px] px-[18px] py-4 flex items-center gap-3.5 text-left press-scale"
        >
            <div className="relative w-10 h-10 rounded-full bg-[var(--color-surface-control)] overflow-hidden shrink-0">
                <Image src={iconUrl || '/wallet/default_icon.webp'} alt={symbol} fill sizes="40px" className="w-full h-full object-cover" />
            </div>
            <div className="flex-1 min-w-0">
                <p className="text-sm font-bold">{name}</p>
                <p className="text-[10px] text-pax-muted mt-0.5">{symbol}</p>
            </div>
            <div className="text-right shrink-0">
                <p className="text-sm font-bold">{balance}</p>
                {valueUsd !== null && valueUsd > 0 && (
                    <p className="text-[11px] text-pax-subtle mt-0.5">{formatUsd(valueUsd)}</p>
                )}
            </div>
        </button>
    );
}
