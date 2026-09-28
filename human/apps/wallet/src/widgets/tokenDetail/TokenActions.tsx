'use client';

/**
 * Action row for the token-detail screen.
 *
 * Layout adapts to the active wallet kind:
 *   - **self-custody / embedded** — 3 buttons: Send, Receive, Swap.
 *   - **funded** — 1 button: Swap. Send and Receive are hidden because
 *     Funded accounts can't move tokens outside the tier whitelist (no
 *     arbitrary sends, no incoming transfers to off-policy addresses).
 *
 * Send delegates to `onSendToken(tokenId)` if provided, otherwise falls back
 * to a generic Send navigation.
 */

import { Send, ArrowDownLeft, Repeat2 } from 'lucide-react';
import { useWalletKind } from '@/providers/WalletKindProvider';
import type { AppRoute } from '@/widgets/shell/useAppRoute';

export interface TokenActionsProps {
    tokenId: string;
    onNavigate: (route: AppRoute) => void;
    onSendToken?: (tokenAddress: string) => void;
}

export function TokenActions({ tokenId, onNavigate, onSendToken }: TokenActionsProps) {
    const { kind } = useWalletKind();
    const isFunded = kind === 'funded';

    const handleSend = () => {
        if (onSendToken) onSendToken(tokenId);
        else onNavigate('send');
    };

    if (isFunded) {
        return (
            <div className="px-3 mt-5">
                <button
                    onClick={() => onNavigate('swap')}
                    className="w-full bg-pax-surface rounded-[20px] py-4 flex items-center justify-center gap-3 press-scale"
                >
                    <Repeat2 className="w-5 h-5 text-pax-accent" />
                    <span className="text-sm font-bold text-pax-subtle">Swap</span>
                </button>
            </div>
        );
    }

    return (
        <div className="grid grid-cols-3 gap-2.5 px-3 mt-5">
            <button
                onClick={handleSend}
                className="bg-pax-surface rounded-[20px] py-4 flex flex-col items-center justify-center gap-2 press-scale"
            >
                <Send className="w-5 h-5 text-pax-accent" />
                <span className="text-xs font-bold text-pax-subtle">Send</span>
            </button>
            <button
                onClick={() => onNavigate('receive')}
                className="bg-pax-surface rounded-[20px] py-4 flex flex-col items-center justify-center gap-2 press-scale"
            >
                <ArrowDownLeft className="w-5 h-5 text-pax-accent" />
                <span className="text-xs font-bold text-pax-subtle">Receive</span>
            </button>
            <button
                onClick={() => onNavigate('swap')}
                className="bg-pax-surface rounded-[20px] py-4 flex flex-col items-center justify-center gap-2 press-scale"
            >
                <Repeat2 className="w-5 h-5 text-pax-accent" />
                <span className="text-xs font-bold text-pax-subtle">Swap</span>
            </button>
        </div>
    );
}
