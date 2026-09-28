'use client';

/**
 * Action bento — the home-screen action grid that fronts the wallet's
 * primary user actions. Layout adapts to the active wallet kind:
 *
 *   - **self-custody / embedded** — 5 buttons: Send, Swap, Bridge,
 *     Receive, Buy. The shipped layout (Bridge is a "coming soon" toast).
 *
 *   - **funded** — 2 buttons: Swap and Trade. Send, Receive, Buy,
 *     Bridge are hidden because Funded accounts are locked to the tier
 *     whitelist (no withdrawals, no fiat in/out, no bridges). Swap still
 *     works for whitelisted token pairs; Trade deep-links to the
 *     Sidiora DEX so users land on the primary funded trading surface.
 */

import { useCallback, useState } from 'react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { useWalletKind } from '@/providers/WalletKindProvider';
import type { AppRoute } from '@/widgets/shell/useAppRoute';

const ACCENT_FILTER =
    'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)';

export interface ActionBentoProps {
    onNavigate: (route: AppRoute) => void;
}

export function ActionBento({ onNavigate }: ActionBentoProps) {
    const { kind } = useWalletKind();
    const [bridgeToast, setBridgeToast] = useState(false);

    const handleBridge = useCallback(() => {
        setBridgeToast(true);
        setTimeout(() => setBridgeToast(false), 2500);
    }, []);

    // ── Funded mode: two-button bento ─────────────────────────────────
    // Same overall footprint (col-span-2, 330×210 aspect) so the page
    // layout doesn't reflow when the kind changes — only the contents do.
    if (kind === 'funded') {
        return (
            <div className="col-span-2 relative" style={{ aspectRatio: '330 / 210' }}>
                <button
                    onClick={() => onNavigate('swap')}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-2 press-scale"
                    style={{ left: 0, top: 0, width: 'calc(50% - 5px)', height: '100%' }}
                >
                    <SvgIcon name="swap" className="w-9 h-9" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-sm font-bold text-pax-subtle">Swap</span>
                    <span className="text-[10px] text-pax-muted px-3 text-center leading-tight">
                        Whitelisted pairs only
                    </span>
                </button>
                <button
                    onClick={() => onNavigate('dex')}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-2 press-scale"
                    style={{ left: '50%', top: 0, width: '50%', height: '100%' }}
                >
                    <SvgIcon name="bridge" className="w-9 h-9" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-sm font-bold text-pax-subtle">Trade</span>
                    <span className="text-[10px] text-pax-muted px-3 text-center leading-tight">
                        Sidiora DEX
                    </span>
                </button>
            </div>
        );
    }

    // ── Standard mode: 5-button bento ─────────────────────────────────
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
                    style={{ left: '39.39%', top: '57.14%', width: 'calc(30.3% - 5px)', height: '42.86%' }}
                >
                    <SvgIcon name="qr-code" className="w-5 h-5" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-xs font-bold text-pax-subtle">Receive</span>
                </button>
                <button
                    onClick={() => onNavigate('ramp')}
                    className="absolute bg-pax-surface rounded-[20px] flex flex-col items-center justify-center gap-1.5 press-scale"
                    style={{ left: '72.73%', top: '57.14%', width: '27.27%', height: '42.86%' }}
                >
                    <SvgIcon name="tokens" className="w-5 h-5" style={{ filter: ACCENT_FILTER }} />
                    <span className="text-xs font-bold text-pax-subtle">Buy</span>
                </button>
            </div>
        </>
    );
}
