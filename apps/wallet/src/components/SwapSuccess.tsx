'use client';

import { useState, useEffect } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { SvgIcon } from '@/components/ui/SvgIcon';
import Image from "next/image";
import { openExternalUrl } from '@/lib/security/navigation';

const draw = {
    hidden: { pathLength: 0, opacity: 0 },
    visible: (i: number) => ({
        pathLength: 1,
        opacity: 1,
        transition: {
            pathLength: {
                delay: i * 0.2,
                type: 'spring',
                duration: 1.5,
                bounce: 0.2,
                ease: [0.22, 1, 0.36, 1],
            },
            opacity: { delay: i * 0.2, duration: 0.3 },
        },
    }),
};

function AnimatedCheckmark({ size = 80 }: { size?: number }) {
    return (
        <motion.svg
            animate="visible"
            height={size}
            initial="hidden"
            viewBox="0 0 100 100"
            width={size}
        >
            <title>Success</title>
            <motion.circle
                custom={0}
                cx="50"
                cy="50"
                r="42"
                stroke="var(--color-status-success)"
                style={{
                    strokeWidth: 2,
                    strokeLinecap: 'round',
                    fill: 'transparent',
                }}
                variants={draw as any}
            />
            <motion.path
                custom={1}
                d="M32 50L45 63L68 35"
                stroke="var(--color-status-success)"
                style={{
                    strokeWidth: 2.5,
                    strokeLinecap: 'round',
                    strokeLinejoin: 'round',
                    fill: 'transparent',
                }}
                variants={draw as any}
            />
        </motion.svg>
    );
}

interface SwapSuccessProps {
    fromAmount: string;
    fromSymbol: string;
    fromIconUrl?: string;
    toAmount: string;
    toSymbol: string;
    toIconUrl?: string;
    txHash: string;
    explorerUrl?: string;
    onExplorerView?: () => void;
    onDone: () => void;
}

export function SwapSuccess({
    fromAmount,
    fromSymbol,
    fromIconUrl,
    toAmount,
    toSymbol,
    toIconUrl,
    txHash,
    explorerUrl,
    onExplorerView,
    onDone,
}: SwapSuccessProps) {
    const [phase, setPhase] = useState<'processing' | 'completed'>('processing');

    useEffect(() => {
        const timer = setTimeout(() => setPhase('completed'), 1400);
        return () => clearTimeout(timer);
    }, []);

    const shortHash = txHash.length > 16
        ? `${txHash.slice(0, 10)}...${txHash.slice(-8)}`
        : txHash;

    return (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-pax-bg/95 backdrop-blur-sm px-6">
            <motion.div
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ duration: 0.5, ease: [0.22, 1, 0.36, 1] }}
                className="w-full max-w-sm flex flex-col items-center"
            >
                {/* Icon */}
                <div className="relative flex items-center justify-center h-[100px] w-[100px] mb-5">
                    <motion.div
                        animate={{ opacity: [0, 0.8, 0.6] }}
                        className="absolute inset-0 rounded-full bg-emerald-500/10 blur-2xl"
                        initial={{ opacity: 0 }}
                        transition={{ duration: 1.5, times: [0, 0.5, 1] }}
                    />
                    <AnimatePresence mode="wait">
                        {phase === 'completed' ? (
                            <motion.div
                                key="check"
                                initial={{ opacity: 0, scale: 0.5, rotate: -90 }}
                                animate={{ opacity: 1, scale: 1, rotate: 0 }}
                                transition={{ duration: 0.5, ease: 'easeOut' }}
                            >
                                <AnimatedCheckmark />
                            </motion.div>
                        ) : (
                            <motion.div
                                key="spinner"
                                exit={{ opacity: 0, scale: 0.5, rotate: 180 }}
                                transition={{ duration: 0.4 }}
                                className="relative"
                            >
                                <motion.div
                                    animate={{ rotate: 360 }}
                                    className="absolute inset-0 rounded-full  "
                                    style={{
                                    }}
                                    transition={{ rotate: { duration: 2, repeat: Infinity, ease: 'linear' } }}
                                />
                                <div className="rounded-full bg-pax-card p-4">
                                    <SvgIcon name="refresh" className="w-8 h-8" style={{ filter: 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }} />
                                </div>
                            </motion.div>
                        )}
                    </AnimatePresence>
                </div>

                {/* Title */}
                <AnimatePresence mode="wait">
                    <motion.h2
                        key={phase}
                        initial={{ opacity: 0, y: 10 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -10 }}
                        transition={{ duration: 0.4 }}
                        className="text-lg font-bold mb-1"
                    >
                        {phase === 'completed' ? 'Swap Completed' : 'Swap in Progress'}
                    </motion.h2>
                </AnimatePresence>

                <AnimatePresence mode="wait">
                    <motion.p
                        key={phase}
                        initial={{ opacity: 0, y: 5 }}
                        animate={{ opacity: 1, y: 0 }}
                        exit={{ opacity: 0, y: -5 }}
                        transition={{ duration: 0.3 }}
                        className="text-xs text-emerald-400 mb-5"
                    >
                        {phase === 'completed' ? shortHash : 'Processing...'}
                    </motion.p>
                </AnimatePresence>

                {/* Swap card */}
                <motion.div
                    initial={{ opacity: 0 }}
                    animate={{ opacity: 1 }}
                    transition={{ delay: 0.2, duration: 0.5 }}
                    className="w-full"
                >
                    <motion.div
                        animate={{ gap: phase === 'completed' ? '0px' : '8px' }}
                        className="flex flex-col"
                        transition={{ duration: 0.5, ease: [0.32, 0.72, 0, 1] }}
                    >
                        {/* You pay */}
                        <div className={`glass-card p-3 transition-all duration-300 ${phase === 'completed' ? 'rounded-b-none ' : ''}`}>
                            <span className="flex items-center gap-1 text-[10px] text-pax-muted mb-1">
                                <SvgIcon name="arrow-up" className="w-3 h-3" /> You paid
                            </span>
                            <div className="flex items-center gap-2">
                                {fromIconUrl ? (
                                    <Image src={fromIconUrl} alt={fromSymbol} className="h-7 w-7 rounded-full bg-white/10 object-cover" onError={(e) => { (e.target as HTMLImageElement).src = '/wallet/default_icon.webp'; }} width={28} height={28} />
                                ) : (
                                    <span className="inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/10 font-medium text-sm text-pax-accent">
                                        {fromSymbol[0]}
                                    </span>
                                )}
                                <div className="flex flex-col">
                                    <motion.span animate={{ opacity: phase === 'completed' ? 1 : 0.5 }} className="text-sm font-medium">
                                        {fromAmount} {fromSymbol}
                                    </motion.span>
                                </div>
                            </div>
                        </div>

                        {/* You receive */}
                        <div className={`glass-card p-3 transition-all duration-300 ${phase === 'completed' ? 'rounded-t-none ' : ''}`}>
                            <span className="flex items-center gap-1 text-[10px] text-pax-muted mb-1">
                                <SvgIcon name="arrow-down" className="w-3 h-3" /> You received
                            </span>
                            <div className="flex items-center gap-2">
                                {toIconUrl ? (
                                    <Image src={toIconUrl} alt={toSymbol} className="h-7 w-7 rounded-full bg-white/10 object-cover" onError={(e) => { (e.target as HTMLImageElement).src = '/wallet/default_icon.webp'; }} width={28} height={28} />
                                ) : (
                                    <span className="inline-flex h-7 w-7 items-center justify-center rounded-lg bg-white/10 font-medium text-sm text-pax-accent">
                                        {toSymbol[0]}
                                    </span>
                                )}
                                <div className="flex flex-col">
                                    <motion.span animate={{ opacity: phase === 'completed' ? 1 : 0.5 }} className="text-sm font-medium text-emerald-400">
                                        {toAmount} {toSymbol}
                                    </motion.span>
                                </div>
                            </div>
                        </div>
                    </motion.div>
                </motion.div>

                {/* Actions */}
                <AnimatePresence>
                    {phase === 'completed' && (
                        <motion.div
                            initial={{ opacity: 0, y: 10 }}
                            animate={{ opacity: 1, y: 0 }}
                            transition={{ delay: 0.3, duration: 0.4 }}
                            className="flex gap-3 mt-6"
                        >
                            {(onExplorerView || explorerUrl) && (
                                <button
                                    onClick={() => onExplorerView ? onExplorerView() : explorerUrl ? openExternalUrl(explorerUrl) : undefined}
                                    className="flex items-center gap-1.5 px-4 py-2.5 rounded-xl bg-white/5 text-xs font-medium press-scale"
                                >
                                    <SvgIcon name="external-link" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} /> Explorer
                                </button>
                            )}
                            <button
                                onClick={onDone}
                                className="px-6 py-2.5 rounded-xl bg-pax-accent text-black text-xs font-semibold press-scale"
                            >
                                Done
                            </button>
                        </motion.div>
                    )}
                </AnimatePresence>
            </motion.div>
        </div>
    );
}
