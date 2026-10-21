'use client';

import { useCallback, useState } from 'react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import type { AppRoute } from '@/widgets/shell/useAppRoute';

const ACCENT_FILTER =
    'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)';

export interface ActionBentoProps {
    onNavigate: (route: AppRoute) => void;
}

export function ActionBento({ onNavigate }: ActionBentoProps) {
    const [bridgeToast, setBridgeToast] = useState(false);

    const handleBridge = useCallback(() => {
        setBridgeToast(true);
        setTimeout(() => setBridgeToast(false), 2500);
    }, []);

    // ── Standard mode: 4-button bento ─────────────────────────────────
    return (
        <>
            {bridgeToast && (
                <div className="fixed top-20 left-1/2 -translate-x-1/2 z-50 px-5 py-2.5 rounded-xl bg-pax-surface   text-sm font-semibold animate-fade-in shadow-2xl">
                    Being integrated
                </div>
            )}
            <div className="col-span-2 relative" style={{ aspectRatio: '330 / 210' }}>
                <button
                    onClick={() => onNavigate('send')}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-1.5 press-scale"
                    style={{ left: 0, top: 0, width: 'calc(36.36% - 5px)', height: 'calc(33.33% - 5px)' }}
                >
                    <SvgIcon name="send" className="w-6 h-6" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-xs font-bold text-pax-subtle">Send</span>
                </button>
                <button
                    onClick={() => onNavigate('swap')}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-2 press-scale"
                    style={{ left: '39.39%', top: 0, width: '60.61%', height: 'calc(52.38% - 5px)' }}
                >
                    <SvgIcon name="swap" className="w-7 h-7" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-sm font-bold text-pax-subtle">Swap</span>
                </button>
                <button
                    onClick={handleBridge}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-1.5 press-scale"
                    style={{ left: 0, top: '38.1%', width: 'calc(36.36% - 5px)', height: '61.9%' }}
                >
                    <SvgIcon name="bridge" className="w-6 h-6" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-[13px] font-bold text-pax-subtle">Bridge</span>
                </button>
                <button
                    onClick={() => onNavigate('receive')}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-1.5 press-scale"
                    style={{ left: '39.39%', top: '57.14%', width: '60.61%', height: '42.86%' }}
                >
                    <SvgIcon name="qr-code" className="w-5 h-5" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-xs font-bold text-pax-subtle">Receive</span>
                </button>
            </div>
        </>
    );
}
