'use client';

import { Send, ArrowDownLeft, Repeat2 } from 'lucide-react';
import type { AppRoute } from '@/widgets/shell/useAppRoute';

export interface TokenActionsProps {
    tokenId: string;
    onNavigate: (route: AppRoute) => void;
    onSendToken?: (tokenAddress: string) => void;
}

export function TokenActions({ tokenId, onNavigate, onSendToken }: TokenActionsProps) {
    const handleSend = () => {
        if (onSendToken) onSendToken(tokenId);
        else onNavigate('send');
    };

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
