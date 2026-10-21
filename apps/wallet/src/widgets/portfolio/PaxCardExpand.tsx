"use client";

import { AnimatePresence, motion } from "framer-motion";
import { Copy, Ellipsis, ExternalLink } from "lucide-react";
import React, { useEffect, useMemo, useRef, useState } from "react";
import Image from "next/image";
import { usePortfolioQuery } from "@/lib/queries";
import { PAX_ICON_URL } from "@/lib/constants";
import { formatBalance, formatUsd } from "@/lib/format";

const CARD_COLORS = [
    'bg-[var(--color-action-primary-muted)]',
    'bg-[var(--color-surface-overlay)]',
    'bg-amber-950',
    'bg-emerald-950',
];

interface WalletItem {
    name: string;
    symbol: string;
    amount: string;
    valueUsd: number | null;
    bgColor: string;
    iconUrl: string | null;
    tokenId: string;
}

export interface PaxCardExpandProps {
    address?: string;
    hidden?: boolean;
    onTokenDetail?: (tokenId: string, symbol: string) => void;
}

export const PaxCardExpand = ({ address, hidden = false, onTokenDetail }: PaxCardExpandProps) => {
    const [expandedCard, setExpandedCard] = useState<number | null>(null);
    const containerRef = useRef<HTMLDivElement>(null);

    const portfolioQuery = usePortfolioQuery(address);
    const portfolio = portfolioQuery.data;

    const walletItems = useMemo((): WalletItem[] => {
        const items: WalletItem[] = [];
        const nativeRaw = portfolio?.native_balance?.balance_raw ?? '0';
        const nativeDecimals = 18;
        const nativeBal = formatBalance(nativeRaw, nativeDecimals, 4);
        const nativeUsd = portfolio?.native_balance?.value_usd ? Number(portfolio.native_balance.value_usd) : null;
        items.push({ name: 'Paxeer', symbol: 'PAX', amount: nativeBal, valueUsd: nativeUsd, bgColor: CARD_COLORS[0], iconUrl: PAX_ICON_URL, tokenId: 'pax' });
        const holdings = portfolio?.token_holdings ?? [];
        holdings.slice(0, 3).forEach((h: any, i: number) => {
            const raw = h.balance_raw ?? '0';
            const dec = h.decimals ?? 18;
            items.push({
                name: h.name || 'Unknown', symbol: h.symbol || '???',
                amount: formatBalance(raw, dec, 4),
                valueUsd: h.value_usd ? Number(h.value_usd) : null,
                bgColor: CARD_COLORS[(i + 1) % CARD_COLORS.length],
                iconUrl: h.icon_url || null,
                tokenId: h.contract_address,
            });
        });
        while (items.length < 4) {
            items.push({ name: '—', symbol: '—', amount: '0', valueUsd: null, bgColor: CARD_COLORS[items.length % CARD_COLORS.length], iconUrl: null, tokenId: '' });
        }
        return items.slice(0, 4);
    }, [portfolio]);

    // Handle outside click
    useEffect(() => {
        const handleClickOutside = (event: MouseEvent) => {
            if (
                containerRef.current &&
                !containerRef.current.contains(event.target as Node)
            ) {
                setExpandedCard(null);
            }
        };

        document.addEventListener("mousedown", handleClickOutside);
        return () => {
            document.removeEventListener("mousedown", handleClickOutside);
        };
    }, []);

    const handleCardClick = (index: number) => {
        setExpandedCard(expandedCard === index ? null : index);
    };

    const renderCard = (item: WalletItem, index: number, isBottomRow = false) => {
        const isExpanded = expandedCard === index;
        const hasThreeBottomCards = expandedCard !== null && getBottomRowCards().length === 3;
        const iconSize = isExpanded ? 'size-12' : hasThreeBottomCards && isBottomRow ? 'size-6' : 'size-8';

        return (
            <motion.div
                key={index}
                layoutId={`card-${index}`}
                onClick={() => !isExpanded && handleCardClick(index)}
                className={`relative flex cursor-pointer flex-col items-start justify-between overflow-hidden p-3 text-white ${isExpanded ? 'h-[180px] w-full' : hasThreeBottomCards && isBottomRow ? 'h-[100px] flex-1' : 'h-[140px] flex-1'
                    } ${item.bgColor}`}
                style={{ transformOrigin: '50% 50% 0px', transform: 'none', borderRadius: '24px' }}
            >
                <div className="flex w-full items-start justify-between">
                    <motion.div layoutId={`icon-${index}`} className={`${iconSize} rounded-full bg-white/10 overflow-hidden shrink-0 flex items-center justify-center`}>
                        {item.iconUrl
                            ? <Image
                                src={item.iconUrl}
                                alt={item.symbol}
                                width={48}
                                height={48}
                                className="w-full h-full object-cover"
                            />
                            : <span className="text-xs font-bold text-white/70">{item.symbol.slice(0, 3)}</span>}
                    </motion.div>

                    {!isExpanded && (
                        <motion.div initial={{ opacity: 0, filter: 'blur(2px)' }} animate={{ opacity: 1, filter: 'blur(0px)' }} exit={{ opacity: 0, filter: 'blur(2px)' }}
                            className="flex size-6 shrink-0 cursor-pointer items-center justify-center rounded-full bg-white/20 p-0.5 transition-colors hover:bg-white/30">
                            <Ellipsis className="size-3" />
                        </motion.div>
                    )}

                    <AnimatePresence>
                        {isExpanded && (
                            <motion.button layoutId={`action-top-${index}`} onClick={() => {
                                if (item.tokenId) navigator.clipboard?.writeText(item.tokenId === 'pax' ? (address || '') : item.tokenId).catch(() => { });
                                setExpandedCard(null);
                            }} className="absolute right-4 top-4 flex items-center gap-2 font-semibold tracking-tight hover:opacity-80">
                                <p className="text-sm">{item.tokenId === 'pax' ? 'Copy Address' : 'Copy Contract'}</p>
                                <div className="flex size-5 shrink-0 items-center justify-center rounded-full bg-white/20 hover:bg-white/30">
                                    <Copy className="size-3" />
                                </div>
                            </motion.button>
                        )}
                    </AnimatePresence>
                    <AnimatePresence>
                        {isExpanded && (
                            <motion.button layoutId={`action-bottom-${index}`} onClick={() => { onTokenDetail?.(item.tokenId, item.symbol); setExpandedCard(null); }}
                                className="absolute bottom-4 right-4 flex items-center gap-1.5 rounded-full bg-white/20 px-3 py-1 text-sm font-semibold tracking-tight hover:bg-white/30">
                                <ExternalLink className="size-3.5" />
                                View Details
                            </motion.button>
                        )}
                    </AnimatePresence>
                </div>
                <div className="flex flex-col items-start justify-center">
                    <motion.span layoutId={`title-${index}`}
                        className={`font-openrunde select-none font-semibold text-white ${isExpanded ? 'text-xl' : hasThreeBottomCards && isBottomRow ? 'text-sm' : 'text-base'
                            }`}>
                        {hidden ? item.symbol : item.name}
                    </motion.span>
                    <motion.span layoutId={`desc-${index}`}
                        className={`font-openrunde select-none font-semibold text-white/50 ${isExpanded ? 'text-lg' : hasThreeBottomCards && isBottomRow ? 'text-xs' : 'text-sm'
                            }`}>
                        {hidden ? '••••' : item.amount}
                    </motion.span>
                    {isExpanded && item.valueUsd !== null && item.valueUsd > 0 && !hidden && (
                        <motion.span className="text-sm text-white/40 mt-0.5">{formatUsd(item.valueUsd)}</motion.span>
                    )}
                </div>
            </motion.div>
        );
    };

    const getTopRowCards = () => {
        if (expandedCard === null) {
            return walletItems.slice(0, 2);
        }
        return walletItems.filter((_, index) => index === expandedCard);
    };

    const getBottomRowCards = () => {
        if (expandedCard === null) {
            return walletItems.slice(2, 4);
        }
        return walletItems.filter((_, index) => index !== expandedCard);
    };

    return (
        <div className="font-open-runde flex h-full w-full flex-col items-center justify-center gap-4 p-4">
            <div
                ref={containerRef}
                className="flex h-[310px] w-full flex-col justify-end gap-4"
            >
                <div className="flex gap-4">
                    {getTopRowCards().map((item, index) => {
                        const originalIndex = expandedCard !== null ? expandedCard : index;
                        return renderCard(item, originalIndex, false);
                    })}
                </div>
                <div className="flex gap-4">
                    {getBottomRowCards().map((item, index) => {
                        const originalIndex =
                            expandedCard !== null
                                ? walletItems.findIndex(
                                    (_, i) => i !== expandedCard && walletItems[i] === item,
                                )
                                : index + 2;
                        return renderCard(item, originalIndex, true);
                    })}
                </div>
            </div>
        </div>
    );
};
