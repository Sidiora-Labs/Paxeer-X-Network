'use client';

/**
 * Round token icon. Falls back to `/wallet/default_icon.webp` if no URL is provided.
 */
import Image from "next/image";
import type { SendableToken } from './useSendableTokens';

export function TokenIcon({ token }: { token: SendableToken }) {
    return (
        <div className="relative w-9 h-9 rounded-full bg-[var(--color-surface-control)] flex items-center justify-center shrink-0 overflow-hidden">
            <Image
                src={token.iconUrl || '/wallet/default_icon.webp'}
                alt={token.symbol}
                className="w-full h-full object-cover rounded-full"
                fill
                sizes="32px"
            />
        </div>
    );
}
