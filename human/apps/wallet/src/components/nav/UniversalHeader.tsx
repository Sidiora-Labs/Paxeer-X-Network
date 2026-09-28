'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { useWalletState, useWalletActions } from '@/providers/WalletProvider';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { shortenAddress, formatBalance, formatUsd } from '@/lib/format';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { cn } from '@/lib/cn';
import { getAvatarPath } from '@/lib/avatar';
import { fetchPortfolio, fetchPaxPriceLatest } from '@/lib/api';
import { PassphrasePrompt } from '@/components/auth/PassphrasePrompt';
import Image from "next/image";
import { useLocale } from '@/providers/LocaleProvider';

export interface UniversalHeaderProps {
    title: string;
    showBack?: boolean;
    onBack?: () => void;
    rightAction?: React.ReactNode;
}

export function UniversalHeader({ title, showBack, onBack, rightAction }: UniversalHeaderProps) {
    const { p, t } = useLocale();
    const { accounts, activeAccount } = useWalletState();
    const { switchAccount, addAccount, renameAccount, deleteAccount, reauthenticate, exportPrivateKey } = useWalletActions();
    const { kind } = useWalletKind();
    const isEmbedded = kind === 'embedded';
    const [open, setOpen] = useState(false);
    const [embeddedSheetOpen, setEmbeddedSheetOpen] = useState(false);
    const [addressCopied, setAddressCopied] = useState(false);
    const [balances, setBalances] = useState<Record<string, number>>({});
    const [editingAddr, setEditingAddr] = useState<string | null>(null);
    const [editName, setEditName] = useState('');
    const editRef = useRef<HTMLInputElement>(null);
    const [menuAddr, setMenuAddr] = useState<string | null>(null);
    const [confirmDeleteAddr, setConfirmDeleteAddr] = useState<string | null>(null);

    // Export private key flow
    const [pkTarget, setPkTarget] = useState<string | null>(null);
    const [pkPassphraseError, setPkPassphraseError] = useState('');
    const [exportedPk, setExportedPk] = useState<{ address: string; key: string } | null>(null);
    const [pkCopied, setPkCopied] = useState(false);

    const handlePkPassphraseComplete = useCallback(async (password: string) => {
        setPkPassphraseError('');
        try {
            await reauthenticate(password);
            const key = await exportPrivateKey(pkTarget!);
            setExportedPk({ address: pkTarget!, key });
            setPkTarget(null);
        } catch (e: unknown) { setPkPassphraseError((e as Error).message || 'Verification failed'); }
    }, [pkTarget, reauthenticate, exportPrivateKey]);

    const closePkReveal = () => { setExportedPk(null); setPkCopied(false); };

    // Fetch balances when sheet opens
    useEffect(() => {
        if (!open || accounts.length === 0) return;
        let cancelled = false;

        const load = async () => {
            const paxPrice = await fetchPaxPriceLatest().catch(() => ({ latest: 0 }));
            const price = paxPrice?.latest ?? 0;

            const results = await Promise.allSettled(
                accounts.map(async (acc) => {
                    const p = await fetchPortfolio(acc.address).catch(() => null);
                    const nativeRaw = p?.native_balance?.balance_raw || '0';
                    const nativeVal = parseFloat(formatBalance(nativeRaw, 18, 6)) * price;
                    const tokenVal = p?.token_holdings?.reduce(
                        (sum: number, h: any) => sum + (h.value_usd ? Number(h.value_usd) : 0),
                        0,
                    ) ?? 0;
                    return { address: acc.address, usd: nativeVal + tokenVal };
                }),
            );

            if (cancelled) return;
            const map: Record<string, number> = {};
            for (const r of results) {
                if (r.status === 'fulfilled') map[r.value.address] = r.value.usd;
            }
            setBalances(map);
        };
        load();
        return () => { cancelled = true; };
    }, [open, accounts]);

    // Focus the edit input when editing starts
    useEffect(() => {
        if (editingAddr && editRef.current) editRef.current.focus();
    }, [editingAddr]);

    const handleSwitch = async (address: string) => {
        if (editingAddr) return;
        await switchAccount(address);
        setOpen(false);
    };

    const handleAdd = async () => {
        await addAccount(`Account ${accounts.length + 1}`);
    };

    const startEdit = (address: string, currentName: string) => {
        setEditingAddr(address);
        setEditName(currentName);
    };

    const commitEdit = async () => {
        if (!editingAddr || !editName.trim()) {
            setEditingAddr(null);
            return;
        }
        await renameAccount(editingAddr, editName.trim());
        setEditingAddr(null);
        setEditName('');
    };

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
                            onClick={() => {
                                // Embedded users have a single managed wallet — open a
                                // minimal address sheet instead of the multi-account
                                // manager.
                                if (isEmbedded) setEmbeddedSheetOpen(true);
                                else setOpen(true);
                            }}
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

            {/* Embedded-mode minimal address sheet (no multi-account UI) */}
            {isEmbedded && embeddedSheetOpen && (
                <>
                    <div
                        className="fixed inset-0 z-[60] bg-black/70 backdrop-blur-md"
                        onClick={() => setEmbeddedSheetOpen(false)}
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
                                <p className="text-[11px] text-pax-accent/80 mt-0.5">Paxeer Wallet · managed custody</p>
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
                                You can manage account-level options (sign out, switch wallet mode) from Settings.
                            </p>
                        </div>
                    </div>
                </>
            )}

            {/* Manage Accounts Full-Screen Sheet (self-custody only) */}
            {!isEmbedded && open && (
                <div className="fixed inset-0 z-[60] flex flex-col">
                    <div className="absolute inset-0 bg-black/70 backdrop-blur-md" onClick={() => { setEditingAddr(null); setOpen(false); }} />

                    <div className="relative flex flex-col w-full max-w-md mx-auto h-full bg-pax-bg animate-slide-up">
                        {/* Header */}
                        <div className="flex items-center justify-between px-4 shrink-0 safe-area-pt" style={{ paddingTop: 'max(1rem, env(safe-area-inset-top, 1rem))' }}>
                            <button
                                onClick={() => { setEditingAddr(null); setOpen(false); }}
                                className="p-1.5 rounded-full bg-white/5 press-scale"
                            >
                                <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
                            </button>
                            <h3 className="text-base font-bold">Manage Accounts</h3>
                            <button
                                onClick={handleAdd}
                                className="p-1.5 rounded-full bg-white/5 press-scale"
                            >
                                <SvgIcon name="plus" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
                            </button>
                        </div>

                        {/* Account List (scrollable) */}
                        <div className="flex-1 overflow-y-auto px-4 pt-3 pb-4 space-y-2">
                            {accounts.map((acc) => {
                                const isActive = acc.address === activeAccount?.address;
                                const isEditing = editingAddr === acc.address;
                                const bal = balances[acc.address];

                                return (
                                    <div
                                        key={acc.address}
                                        className={cn(
                                            'flex items-center gap-3 px-3.5 py-3.5 rounded-2xl transition-all',
                                            isActive
                                                ? 'bg-pax-accent/10'
                                                : 'bg-white/[0.04] hover:bg-white/[0.07]',
                                        )}
                                    >
                                        {/* Avatar */}
                                        <button
                                            onClick={() => handleSwitch(acc.address)}
                                            className="shrink-0 press-scale"
                                        >
                                            <Image
                                                src={getAvatarPath(acc.address)}
                                                alt={acc.name || 'Account'}
                                                width={40}
                                                height={40}
                                                className="w-10 h-10 rounded-full object-cover"
                                            />
                                        </button>

                                        {/* Name + Address */}
                                        <button
                                            onClick={() => handleSwitch(acc.address)}
                                            className="flex-1 text-left min-w-0"
                                        >
                                            {isEditing ? (
                                                <input
                                                    ref={editRef}
                                                    type="text"
                                                    value={editName}
                                                    onChange={(e) => setEditName(e.target.value)}
                                                    onBlur={commitEdit}
                                                    onKeyDown={(e) => { if (e.key === 'Enter') commitEdit(); if (e.key === 'Escape') setEditingAddr(null); }}
                                                    onClick={(e) => e.stopPropagation()}
                                                    className="w-full bg-transparent text-sm font-semibold outline-none   pb-0.5"
                                                />
                                            ) : (
                                                <p className="text-sm font-semibold truncate">{acc.name}</p>
                                            )}
                                            <p className="text-[11px] text-pax-muted truncate mt-0.5">
                                                {shortenAddress(acc.address, 6)}
                                            </p>
                                        </button>

                                        {/* Balance */}
                                        <div className="text-right shrink-0 mr-1">
                                            {bal !== undefined ? (
                                                <p className="text-sm font-medium text-pax-muted">{formatUsd(bal)}</p>
                                            ) : (
                                                <div className="w-12 h-4 rounded bg-white/5 animate-pulse" />
                                            )}
                                        </div>

                                        {/* Actions */}
                                        {isEditing ? (
                                            <button
                                                onClick={(e) => { e.stopPropagation(); commitEdit(); }}
                                                className="p-1.5 rounded-full bg-pax-accent/10 press-scale shrink-0"
                                            >
                                                <SvgIcon name="check" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                                            </button>
                                        ) : (
                                            <div className="relative shrink-0">
                                                <button
                                                    onClick={(e) => { e.stopPropagation(); setMenuAddr(menuAddr === acc.address ? null : acc.address); }}
                                                    className="p-1.5 rounded-full bg-white/5 press-scale"
                                                >
                                                    <SvgIcon name="more-vertical" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                                </button>
                                                {menuAddr === acc.address && (
                                                    <>
                                                        <div className="fixed inset-0 z-[70]" onClick={(e) => { e.stopPropagation(); setMenuAddr(null); }} />
                                                        <div className="absolute right-0 top-full mt-1 w-48 bg-pax-card   rounded-xl shadow-2xl z-[80] overflow-hidden animate-scale-in">
                                                            <button
                                                                onClick={(e) => { e.stopPropagation(); setMenuAddr(null); startEdit(acc.address, acc.name || ''); }}
                                                                className="w-full flex items-center gap-2.5 px-3.5 py-3 text-sm hover:bg-white/5 transition-colors"
                                                            >
                                                                <SvgIcon name="pencil" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                                                Rename
                                                            </button>
                                                            <button
                                                                onClick={(e) => { e.stopPropagation(); setMenuAddr(null); setPkPassphraseError(''); setPkTarget(acc.address); }}
                                                                className="w-full flex items-center gap-2.5 px-3.5 py-3 text-sm hover:bg-white/5 transition-colors"
                                                            >
                                                                <SvgIcon name="key" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                                                Export Private Key
                                                            </button>
                                                            {accounts.length > 1 && (
                                                                <button
                                                                    onClick={(e) => { e.stopPropagation(); setMenuAddr(null); setConfirmDeleteAddr(acc.address); }}
                                                                    className="w-full flex items-center gap-2.5 px-3.5 py-3 text-sm text-red-400 hover:bg-red-500/5 transition-colors"
                                                                >
                                                                    <SvgIcon name="trash" className="w-3.5 h-3.5" style={{ filter: 'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)' }} />
                                                                    Remove
                                                                </button>
                                                            )}
                                                        </div>
                                                    </>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                );
                            })}
                        </div>

                        {/* Bottom Add Account Button */}
                        <div className="shrink-0 px-4 pb-8 pt-3" style={{ paddingBottom: 'max(2rem, env(safe-area-inset-bottom, 1.5rem))' }}>
                            <button
                                onClick={handleAdd}
                                className="w-full flex items-center justify-center gap-2 py-3.5 rounded-2xl bg-white/[0.06] text-sm font-medium press-scale hover:bg-white/[0.09] transition-colors"
                            >
                                <SvgIcon name="plus" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} />
                                Add Account
                            </button>
                        </div>
                    </div>

                    {pkTarget && (
                        <PassphrasePrompt
                            title={t.settings.freshAuth}
                            subtitle={p.enterPin}
                            error={pkPassphraseError}
                            onSubmit={handlePkPassphraseComplete}
                            onCancel={() => { setPkTarget(null); setPkPassphraseError(''); }}
                        />
                    )}

                    {/* Private key reveal screen */}
                    {exportedPk && (
                        <>
                            <div className="fixed inset-0 z-[190] bg-black/60" />
                            <div className="fixed inset-0 z-[200] flex items-center justify-center px-6">
                                <div className="w-full max-w-xs bg-pax-bg rounded-2xl p-6 space-y-4 animate-scale-in">
                                    <div className="flex items-center justify-between">
                                        <h2 className="text-base font-bold">Private Key</h2>
                                        <button onClick={closePkReveal} className="p-1.5 rounded-full bg-white/5 press-scale">
                                            <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
                                        </button>
                                    </div>

                                    {/* Scam warning */}
                                    <div className="flex gap-2.5 p-3 rounded-xl bg-red-500/10  ">
                                        <SvgIcon name="alert-triangle" className="w-4 h-4 shrink-0 mt-0.5" style={{ filter: 'invert(48%) sepia(79%) saturate(2476%) hue-rotate(338deg) brightness(118%) contrast(119%)' }} />
                                        <p className="text-[11px] text-red-400 leading-relaxed">
                                            <span className="font-bold">Never share your private key.</span> Anyone with this key has full control of your funds. Paxeer support will <span className="font-bold">never</span> ask for it. Sharing it with anyone is a scam.
                                        </p>
                                    </div>

                                    {/* Key display */}
                                    <div className="bg-pax-surface rounded-xl p-3.5">
                                        <p className="text-[11px] font-mono break-all text-pax-subtle leading-relaxed select-all">
                                            {exportedPk.key}
                                        </p>
                                    </div>

                                    <button
                                        onClick={async () => {
                                            await navigator.clipboard.writeText(exportedPk.key);
                                            setPkCopied(true);
                                            setTimeout(() => setPkCopied(false), 2000);
                                        }}
                                        className="w-full flex items-center justify-center gap-2 py-3 rounded-xl bg-pax-accent/10 text-sm font-medium text-pax-accent press-scale transition-colors"
                                    >
                                        <SvgIcon name={pkCopied ? 'check' : 'copy'} className="w-4 h-4" style={{ filter: pkCopied ? 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' : 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
                                        {pkCopied ? 'Copied!' : 'Copy Private Key'}
                                    </button>

                                    <button onClick={closePkReveal} className="w-full py-2.5 rounded-xl bg-white/5 text-sm font-medium press-scale">
                                        Done
                                    </button>
                                </div>
                            </div>
                        </>
                    )}

                    {/* Delete confirmation dialog */}
                    {confirmDeleteAddr && (
                        <>
                            <div className="fixed inset-0 z-[90] bg-black/60" onClick={() => setConfirmDeleteAddr(null)} />
                            <div className="fixed inset-0 z-[100] flex items-center justify-center px-8">
                                <div className="w-full max-w-xs bg-pax-card rounded-2xl p-5 space-y-4 animate-scale-in">
                                    <h3 className="text-base font-bold text-center">Remove Account?</h3>
                                    <p className="text-xs text-pax-muted text-center">
                                        This account will be removed from your wallet. You can re-add it later from your recovery phrase.
                                    </p>
                                    <div className="flex gap-3">
                                        <button
                                            onClick={() => setConfirmDeleteAddr(null)}
                                            className="flex-1 py-2.5 rounded-xl bg-white/5 text-sm font-medium press-scale"
                                        >
                                            Cancel
                                        </button>
                                        <button
                                            onClick={async () => {
                                                await deleteAccount(confirmDeleteAddr);
                                                setConfirmDeleteAddr(null);
                                            }}
                                            className="flex-1 py-2.5 rounded-xl bg-red-500/15 text-red-400 text-sm font-medium press-scale"
                                        >
                                            Remove
                                        </button>
                                    </div>
                                </div>
                            </div>
                        </>
                    )}
                </div>
            )}
        </>
    );
}
