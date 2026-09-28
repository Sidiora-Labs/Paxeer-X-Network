'use client';

import { motion, AnimatePresence } from 'framer-motion';
import { Check, X } from 'lucide-react';
import { useWalletState, useWalletActions } from '@/providers/WalletProvider';
import { shortenAddress } from '@/lib/format';
import { getAvatarPath } from '@/lib/avatar';
import { useLocale } from '@/providers/LocaleProvider';
import Image from "next/image";

interface AccountSwitcherDrawerProps {
    open: boolean;
    onClose: () => void;
}

export function AccountSwitcherDrawer({ open, onClose }: AccountSwitcherDrawerProps) {
    const { accounts, activeAccount } = useWalletState();
    const { switchAccount } = useWalletActions();
    const { t } = useLocale();

    const handleSwitch = (address: string) => {
        if (address === activeAccount?.address) { onClose(); return; }
        switchAccount(address);
        onClose();
    };

    return (
        <AnimatePresence>
            {open && (
                <div className="fixed inset-0 z-[60] flex items-end justify-center">
                    <motion.div
                        key="account-backdrop"
                        initial={{ opacity: 0 }}
                        animate={{ opacity: 1 }}
                        exit={{ opacity: 0 }}
                        transition={{ duration: 0.2 }}
                        className="absolute inset-0 bg-black/60 backdrop-blur-sm"
                        onClick={onClose}
                    />
                    <motion.div
                        key="account-sheet"
                        initial={{ y: '100%', opacity: 0 }}
                        animate={{ y: 0, opacity: 1 }}
                        exit={{ y: '100%', opacity: 0 }}
                        transition={{ type: 'spring', stiffness: 300, damping: 30, mass: 0.8 }}
                        className="relative w-full max-w-md bg-pax-card rounded-t-3xl p-5 z-[61]"
                        style={{ maxHeight: '70vh', display: 'flex', flexDirection: 'column', paddingBottom: 'max(1.5rem, calc(env(safe-area-inset-bottom, 0px) + 1.5rem))' }}
                    >
                        <div className="w-10 h-1 rounded-full bg-white/10 absolute top-2.5 left-1/2 -translate-x-1/2" />

                        <div className="flex items-center justify-between mb-5 pt-2">
                            <h3 className="text-base font-bold">{t.account.switchAccount}</h3>
                            <button
                                onClick={onClose}
                                className="p-1.5 rounded-full bg-white/5 press-scale"
                                aria-label={t.common.close}
                            >
                                <X className="w-4 h-4 text-pax-muted" />
                            </button>
                        </div>

                        <div className="space-y-1.5 overflow-y-auto flex-1">
                            {accounts.map((account) => {
                                const isActive = account.address === activeAccount?.address;
                                return (
                                    <button
                                        key={account.address}
                                        onClick={() => handleSwitch(account.address)}
                                        className={`w-full flex items-center gap-3 px-4 py-3.5 rounded-2xl press-scale transition-all ${isActive
                                                ? 'bg-pax-accent/10  '
                                                : 'bg-white/[0.04] hover:bg-white/[0.07]'
                                            }`}
                                    >
                                        <Image
                                            src={getAvatarPath(account.address)}
                                            alt={account.name}
                                            width={40}
                                            height={40}
                                            className="w-10 h-10 rounded-full object-cover shrink-0"
                                        />
                                        <div className="flex-1 text-left min-w-0">
                                            <p className="text-sm font-semibold truncate">{account.name}</p>
                                            <p className="text-xs text-pax-muted font-mono">
                                                {shortenAddress(account.address, 6)}
                                            </p>
                                        </div>
                                        {isActive && (
                                            <div className="w-6 h-6 rounded-full bg-pax-accent flex items-center justify-center shrink-0">
                                                <Check className="w-3.5 h-3.5 text-black" />
                                            </div>
                                        )}
                                    </button>
                                );
                            })}
                        </div>
                    </motion.div>
                </div>
            )}
        </AnimatePresence>
    );
}
