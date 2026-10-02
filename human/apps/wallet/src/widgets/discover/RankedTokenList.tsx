'use client';

import { TrendingUp, TrendingDown, Loader2 } from 'lucide-react';
import { formatPrice } from '@/lib/format';
import type { EnrichedRankedToken } from '@/lib/queries/rankings';
import Image from "next/image";

interface RankedTokenListProps {
    ranked: EnrichedRankedToken[];
    loading: boolean;
    onTokenTrade?: (poolAddress: string, symbol?: string) => void;
}

export function RankedTokenList({ ranked, loading, onTokenTrade }: RankedTokenListProps) {
    if (loading) {
        return (
            <div className="col-span-2 flex items-center justify-center py-8">
                <Loader2 className="w-5 h-5 text-pax-muted animate-spin" />
            </div>
        );
    }

    if (ranked.length === 0) {
        return (
            <div className="col-span-2 bg-pax-surface rounded-[20px] py-8 text-center">
                <p className="text-xs text-pax-muted">No tokens found</p>
            </div>
        );
    }

    return (
        <>
            {ranked.slice(0, 1).map((token) => {
                const positive = token.priceChange24h >= 0;
                return (
                    <button
                        key={token.poolAddress}
                        onClick={() => onTokenTrade?.(token.poolAddress, token.symbol)}
                        className="col-span-2 bg-pax-surface rounded-[20px] px-[18px] py-4 flex items-center gap-3.5 text-left press-scale"
                    >
                        <div className="relative w-10 h-10 rounded-full bg-[var(--color-surface-control)] overflow-hidden shrink-0">
                            <Image src={token.logoUrl || '/wallet/default_icon.webp'} alt={token.symbol} fill sizes="40px" className="w-full h-full object-cover rounded-full" onError={(e) => { (e.target as HTMLImageElement).src = '/wallet/default_icon.webp'; }} />
                        </div>
                        <div className="flex-1 min-w-0">
                            <div className="flex items-center gap-1.5">
                                <p className="text-sm font-bold truncate">{token.symbol}</p>
                                <span className="text-[9px] font-bold text-pax-muted">#{token.rank}</span>
                            </div>
                            <p className="text-[11px] text-pax-muted truncate">{token.name}</p>
                        </div>
                        <div className="text-right shrink-0">
                            <p className="text-sm font-bold">{formatPrice(token.price)}</p>
                            <p className={`text-[10px] font-bold flex items-center justify-end gap-0.5 ${positive ? 'text-pax-success' : 'text-pax-error'}`}>
                                {positive ? <TrendingUp className="w-2.5 h-2.5" /> : <TrendingDown className="w-2.5 h-2.5" />}
                                {positive ? '+' : ''}{token.priceChange24h.toFixed(1)}%
                            </p>
                        </div>
                    </button>
                );
            })}

            {ranked.slice(1, 3).map((token) => {
                const positive = token.priceChange24h >= 0;
                return (
                    <button
                        key={token.poolAddress}
                        onClick={() => onTokenTrade?.(token.poolAddress, token.symbol)}
                        className="bg-pax-surface rounded-[20px] p-4 flex flex-col justify-between text-left press-scale min-h-[110px]"
                    >
                        <div className="flex items-center gap-2.5">
                            <div className="relative w-[30px] h-[30px] rounded-full bg-[var(--color-surface-control)] overflow-hidden shrink-0">
                                <Image src={token.logoUrl || '/wallet/default_icon.webp'} alt={token.symbol} fill sizes="40px" className="w-full h-full object-cover rounded-full" onError={(e) => { (e.target as HTMLImageElement).src = '/wallet/default_icon.webp'; }} />
                            </div>
                            <div className="min-w-0">
                                <p className="text-[13px] font-bold truncate">{token.symbol}</p>
                                <p className="text-[10px] text-pax-muted">#{token.rank}</p>
                            </div>
                        </div>
                        <div className="mt-auto pt-2">
                            <p className="text-[15px] font-bold">{formatPrice(token.price)}</p>
                            <p className={`text-[10px] font-bold mt-0.5 ${positive ? 'text-pax-success' : 'text-pax-error'}`}>
                                {positive ? '+' : ''}{token.priceChange24h.toFixed(1)}%
                            </p>
                        </div>
                    </button>
                );
            })}

            {ranked.length > 3 && (
                <div className="col-span-2 bg-pax-surface rounded-[20px]  divide-white/[0.04] overflow-hidden">
                    {ranked.slice(3).map((token) => {
                        const positive = token.priceChange24h >= 0;
                        return (
                            <button
                                key={token.poolAddress}
                                onClick={() => onTokenTrade?.(token.poolAddress, token.symbol)}
                                className="w-full flex items-center gap-3 px-4 py-3 press-scale text-left"
                            >
                                <div className="relative w-8 h-8 rounded-full bg-[var(--color-surface-control)] overflow-hidden shrink-0">
                                    <Image src={token.logoUrl || '/wallet/default_icon.webp'} alt={token.symbol} fill sizes="40px" className="w-full h-full object-cover rounded-full" onError={(e) => { (e.target as HTMLImageElement).src = '/wallet/default_icon.webp'; }} />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-1.5">
                                        <p className="text-[13px] font-semibold truncate">{token.symbol}</p>
                                        <span className="text-[9px] font-bold text-pax-muted">#{token.rank}</span>
                                    </div>
                                    <p className="text-[10px] text-pax-muted truncate">{token.name}</p>
                                </div>
                                <div className="text-right shrink-0">
                                    <p className="text-xs font-semibold">{formatPrice(token.price)}</p>
                                    <p className={`text-[10px] font-semibold flex items-center justify-end gap-0.5 ${positive ? 'text-pax-success' : 'text-pax-error'}`}>
                                        {positive ? <TrendingUp className="w-2.5 h-2.5" /> : <TrendingDown className="w-2.5 h-2.5" />}
                                        {positive ? '+' : ''}{token.priceChange24h.toFixed(1)}%
                                    </p>
                                </div>
                            </button>
                        );
                    })}
                </div>
            )}
        </>
    );
}
