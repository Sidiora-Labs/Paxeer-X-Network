'use client';

/**
 * Small round token badge used in swap inputs and confirm drawer.
 */

import type { SwapToken } from '@/lib/swap';
import Image from "next/image";

export interface TokenBadgeProps {
    token: SwapToken;
    size?: 'sm' | 'md';
}

export function TokenBadge({ token, size = 'md' }: TokenBadgeProps) {
    const dim = size === 'sm' ? 'w-5 h-5' : 'w-9 h-9';
    return (
        <div
            className={`relative ${dim} rounded-full bg-[var(--color-surface-control)] flex items-center justify-center shrink-0 overflow-hidden`}
        >
            <Image
                src={token.iconUrl || '/default_icon.webp'}
                alt={token.symbol}
                className="w-full h-full object-cover rounded-full"
                fill
                sizes="32px"
            />
        </div>
    );
}
