'use client';

import { useState } from 'react';
import { useWalletState } from '@/providers/WalletProvider';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { shortenAddress } from '@/lib/format';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { getAvatarPath } from '@/lib/avatar';
import Image from "next/image";

export interface UniversalHeaderProps {
    title: string;
    showBack?: boolean;
    onBack?: () => void;
    rightAction?: React.ReactNode;
}

export function UniversalHeader({ title, showBack, onBack, rightAction }: UniversalHeaderProps) {
    const { activeAccount } = useWalletState();
    const { kind } = useWalletKind();
    const isEmbedded = kind === 'embedded';
    const [sheetOpen, setSheetOpen] = useState(false);
    const [addressCopied, setAddressCopied] = useState(false);

    return (
        <>
            <header className="fixed top-0 left-0 right-0 z-40 bg-pax-bg/90 backdrop-blur-xl safe-area-pt">
                <div className="relative flex items-center justify-between px-4 h-14">
                    {/* Left side */}
                    {showBack ? (
                        <button type="button" onClick={onBack} aria-label="Back" className="p-2 -ml-2 press-scale z-10">
                            <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                        </button>
                    ) : (
                        <button
                            onClick={() => setSheetOpen(true)}
                            className="flex items-center gap-2 press-scale z-10"
                        >
                            <Image
                                src={getAvatarPath(activeAccount?.address || '')}
                                alt={activeAccount?.name || 'Account'}
                                width={32}
                                height={32}
                                className="w-8 h-8 rounded-full object-cover"
                            />
                            <div className="text-left">
                                <p className="text-sm font-semibold leading-tight truncate max-w-[140px]">{activeAccount?.name || 'Account 1'}</p>
                                <p className="text-[10px] text-pax-muted leading-tight">
                                    {activeAccount ? shortenAddress(activeAccount.address) : '—'}
                                </p>
                            </div>
                            <SvgIcon name="chevron-down" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                        </button>
                    )}

                    {/* Center title */}
                    <h1 className="absolute inset-x-0 text-center text-sm font-bold pointer-events-none">
                        {title}
                    </h1>

                    {/* Right side */}
                    <div className="z-10 shrink-0">
                        {rightAction || <div className="w-9" />}
                    </div>
                </div>
            </header>

            {sheetOpen && (
                <>
                    <div
                        className="fixed inset-0 z-[60] bg-black/70 backdrop-blur-md"
                        onClick={() => setSheetOpen(false)}
                    />
                    <div className="fixed inset-x-0 bottom-0 z-[70] bg-pax-bg rounded-t-3xl p-5 pb-8 animate-slide-up safe-area-pb max-w-md mx-auto">
                        <div className="flex flex-col items-center gap-4">
                            <div className="w-10 h-1 rounded-full bg-white/15 -mt-1" />
                            <Image
                                src={getAvatarPath(activeAccount?.address || '')}
                                alt={activeAccount?.name || 'Account'}
                                width={64}
                                height={64}
                                className="w-16 h-16 rounded-full object-cover"
                            />
                            <div className="text-center">
                                <p className="text-base font-bold truncate max-w-[260px]">{activeAccount?.name || 'Paxeer Wallet'}</p>
                                <p className="text-[11px] text-pax-accent/80 mt-0.5">{isEmbedded ? 'Paxeer Wallet · managed custody' : 'Funded Account'}</p>
                            </div>
                            <button
                                onClick={async () => {
                                    if (!activeAccount?.address) return;
                                    await navigator.clipboard.writeText(activeAccount.address);
                                    setAddressCopied(true);
                                    setTimeout(() => setAddressCopied(false), 1500);
                                }}
                                className="w-full flex items-center gap-2 px-4 py-3 rounded-xl bg-white/[0.06] press-scale hover:bg-white/[0.09] transition-colors"
                            >
                                <SvgIcon name="copy" className="w-4 h-4 shrink-0" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                <span className="flex-1 text-left text-xs font-mono truncate">{activeAccount?.address}</span>
                                {addressCopied && (
                                    <SvgIcon
                                        name="check"
                                        className="w-4 h-4 shrink-0"
                                        style={{ filter: 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }}
                                    />
                                )}
                            </button>
                            <p className="text-[11px] text-pax-muted text-center leading-relaxed">
                                You can manage account-level options from Settings.
                            </p>
                        </div>
                    </div>
                </>
            )}

        </>
    );
}
