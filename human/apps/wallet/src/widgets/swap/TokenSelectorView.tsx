'use client';

/**
 * Full-page token picker rendered when the swap widget enters
 * `selectFrom` / `selectTo` view.
 *
 * Local filter on the in-memory tokens list, plus an optional debounced
 * paxscan search for the output side that returns extra results from the
 * tokens index.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { searchTokens } from '@/lib/api';
import type { SwapToken } from '@/lib/swap';
import { TokenBadge } from './TokenBadge';
import { paxscanToSwapToken } from './util';
import Image from "next/image";

const ACCENT_FILTER =
    'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)';

export interface TokenSelectorViewProps {
    title: string;
    tokens: SwapToken[];
    selected: SwapToken;
    exclude: SwapToken;
    onSelect: (t: SwapToken) => void;
    onClose: () => void;
    /** Enable paxscan-backed search for the output side. */
    enableSearch?: boolean;
}

export function TokenSelectorView({
    title,
    tokens,
    selected,
    exclude,
    onSelect,
    onClose,
    enableSearch = false,
}: TokenSelectorViewProps) {
    const [query, setQuery] = useState('');
    const [searchResults, setSearchResults] = useState<SwapToken[]>([]);
    const [searching, setSearching] = useState(false);
    const searchRef = useRef<ReturnType<typeof setTimeout>>();

    // Debounced paxscan search.
    useEffect(() => {
        if (!enableSearch || !query.trim()) {
            setSearchResults([]);
            return;
        }
        if (searchRef.current) clearTimeout(searchRef.current);
        setSearching(true);
        searchRef.current = setTimeout(async () => {
            try {
                const results = await searchTokens(query);
                setSearchResults(results.map(paxscanToSwapToken));
            } catch {
                setSearchResults([]);
            } finally {
                setSearching(false);
            }
        }, 400);
        return () => {
            if (searchRef.current) clearTimeout(searchRef.current);
        };
    }, [query, enableSearch]);

    // Local filter applied to the in-memory list.
    const filteredTokens = useMemo(() => {
        if (!query.trim()) return tokens;
        const q = query.trim().toLowerCase();
        return tokens.filter(
            (t) =>
                t.symbol.toLowerCase().includes(q) ||
                t.name.toLowerCase().includes(q) ||
                t.address.toLowerCase().includes(q),
        );
    }, [tokens, query]);

    // Merge local filter results with paxscan search results, deduped by address.
    const displayTokens = useMemo(() => {
        if (!query.trim()) return tokens;
        const seen = new Set(filteredTokens.map((t) => t.address.toLowerCase() || 'native'));
        const merged = [...filteredTokens];
        for (const t of searchResults) {
            const key = t.address.toLowerCase();
            if (!seen.has(key)) {
                seen.add(key);
                merged.push(t);
            }
        }
        return merged;
    }, [tokens, filteredTokens, searchResults, query]);

    const isSelected = (t: SwapToken) =>
        t.symbol === selected.symbol && t.address === selected.address;
    const isExcluded = (t: SwapToken) =>
        t.symbol === exclude.symbol && t.address === exclude.address;

    return (
        <div className="px-4 pt-4 pb-24">
            <div className="flex items-center justify-between mb-4">
                <h2 className="text-lg font-bold">{title}</h2>
                <button onClick={onClose} className="p-1.5 rounded-full bg-white/5 press-scale">
                    <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
                </button>
            </div>

            <div className="relative mb-4">
                <Image
                    src="/ui_icons/search.svg"
                    alt=""
                    className="absolute left-3.5 top-1/2 -translate-y-1/2 w-4 h-4"
                    style={{ filter: 'brightness(0) invert(0.6)' }}
                    width={16}
                    height={16}
                />
                <input
                    type="text"
                    value={query}
                    onChange={(e) => setQuery(e.target.value)}
                    placeholder={enableSearch ? 'Search by name or paste address...' : 'Filter tokens...'}
                    className="w-full pl-10 pr-4 py-3 rounded-xl bg-white/5   text-sm outline-none  transition-colors placeholder:text-white/20"
                />
                {query && (
                    <button
                        onClick={() => setQuery('')}
                        className="absolute right-3 top-1/2 -translate-y-1/2 p-0.5"
                    >
                        <SvgIcon
                            name="x"
                            className="w-3.5 h-3.5"
                            style={{ filter: 'brightness(0) invert(0.6)' }}
                        />
                    </button>
                )}
            </div>

            {searching && (
                <div className="flex items-center gap-2 px-1 mb-3">
                    <Loader2 className="w-3.5 h-3.5 animate-spin text-pax-muted" />
                    <span className="text-xs text-pax-muted">Searching tokens...</span>
                </div>
            )}

            <div className="space-y-1.5">
                {displayTokens.length === 0 && !searching && query.trim() && (
                    <div className="py-8 text-center">
                        <p className="text-xs text-pax-muted">No tokens found for &ldquo;{query}&rdquo;</p>
                    </div>
                )}
                {displayTokens
                    .filter((t) => !isExcluded(t))
                    .map((t) => {
                        const sel = isSelected(t);
                        return (
                            <button
                                key={t.address || 'native'}
                                onClick={() => onSelect(t)}
                                className={`w-full flex items-center gap-3 px-3.5 py-3.5 rounded-xl transition-all press-scale ${sel ? 'bg-pax-accent/10' : 'bg-white/5 hover:bg-white/8'
                                    }`}
                            >
                                <TokenBadge token={t} />
                                <div className="flex-1 text-left min-w-0">
                                    <p className="text-sm font-medium">{t.symbol}</p>
                                    <p className="text-[11px] text-pax-muted truncate">{t.name}</p>
                                </div>
                                {t.isStablecoin && (
                                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-blue-500/10 text-blue-400">
                                        Stable
                                    </span>
                                )}
                                {t.isNative && (
                                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-pax-accent/10 text-pax-accent">
                                        Native
                                    </span>
                                )}
                                {t.isLaunchpad && (
                                    <span className="text-[10px] px-1.5 py-0.5 rounded bg-pax-accent/10 text-pax-accent">
                                        Launchpad
                                    </span>
                                )}
                                {sel && (
                                    <SvgIcon name="check" className="w-4 h-4" style={{ filter: ACCENT_FILTER }} />
                                )}
                            </button>
                        );
                    })}
            </div>
        </div>
    );
}
