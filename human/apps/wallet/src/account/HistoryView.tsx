'use client';

import { useCallback, useEffect, useRef, useState } from 'react';
import type { HistoryItem } from '@paxeer/wallet';
import { Button, ErrorState, Skeleton } from '@/components/ui/primitives';
import { shortenAddress } from '@/lib/format';
import { useAccountClients } from './AccountProvider';
import { units } from './format';
import { errorMessage } from './hooks';
import { TransactionLadder } from './StatusLadder';

interface HistoryState {
    readonly items: readonly HistoryItem[];
    readonly cursor: string | null;
    readonly started: boolean;
    readonly loading: boolean;
    readonly error: unknown;
}

const INITIAL: HistoryState = { items: [], cursor: null, started: false, loading: true, error: null };
const TX_HASH = /^0x[0-9a-fA-F]{64}$/;

function assetLabel(item: HistoryItem): string {
    return item.asset_metadata?.symbol ?? (item.asset.startsWith('evm:') ? shortenAddress(item.asset.slice(4)) : shortenAddress(item.asset, 6));
}

export function HistoryView({ account, pageSize = 20 }: { account: string; pageSize?: number }) {
    const { endpoint } = useAccountClients();
    const [state, setState] = useState<HistoryState>(INITIAL);
    const [selected, setSelected] = useState<string | null>(null);
    const alive = useRef(true);

    const load = useCallback(
        async (cursor: string | null, append: boolean) => {
            setState((prev) => ({ ...prev, loading: true, error: null }));
            try {
                const page = await endpoint.getUnifiedHistory(account, cursor, { limit: pageSize });
                if (!alive.current) return;
                setState((prev) => ({
                    items: append ? [...prev.items, ...page.items] : page.items,
                    cursor: page.next_cursor,
                    started: true,
                    loading: false,
                    error: null,
                }));
            } catch (error) {
                if (!alive.current) return;
                setState((prev) => ({ ...prev, loading: false, error }));
            }
        },
        [endpoint, account, pageSize],
    );

    useEffect(() => {
        alive.current = true;
        setState(INITIAL);
        void load(null, false);
        return () => {
            alive.current = false;
        };
    }, [load]);

    return (
        <section aria-label="History" className="space-y-3 rounded-[20px] bg-pax-surface p-4">
            <h2 className="text-sm font-bold">History</h2>
            {state.items.length === 0 && state.loading && <Skeleton className="h-24 w-full" />}
            {state.started && state.items.length === 0 && !state.loading && !state.error && (
                <p className="text-xs text-pax-muted">No activity yet.</p>
            )}
            <ul aria-label="History items" className="divide-y divide-white/[0.06]">
                {state.items.map((item) => {
                    const decimals = item.asset_metadata?.decimals ?? null;
                    const symbol = assetLabel(item);
                    const ladder = item.side === 'paxeer' && TX_HASH.test(item.tx_id);
                    return (
                        <li key={item.id} data-item={item.id} data-side={item.side} className="space-y-2 py-3">
                            <div className="flex items-start justify-between gap-3">
                                <div>
                                    <p className="text-sm font-semibold">
                                        {item.direction === 'in' ? 'Received' : 'Sent'} {symbol}
                                    </p>
                                    <p className="text-[11px] text-pax-muted">
                                        {item.side === 'paxeer' ? 'Paxeer' : 'LayerX'} · {item.kind.replace(/_/g, ' ')}
                                        {item.counterparty ? ` · ${shortenAddress(item.counterparty)}` : ''}
                                    </p>
                                </div>
                                <p className="text-right text-xs tabular-nums">
                                    {item.direction === 'in' ? '+' : '-'}
                                    {units(item.amount, decimals)} {symbol}
                                </p>
                            </div>
                            {ladder && (
                                <Button
                                    variant="quiet"
                                    className="min-h-0 px-0 py-0 text-xs"
                                    aria-expanded={selected === item.id}
                                    onClick={() => setSelected((prev) => (prev === item.id ? null : item.id))}
                                >
                                    Status
                                </Button>
                            )}
                            {ladder && selected === item.id && <TransactionLadder hash={item.tx_id} />}
                        </li>
                    );
                })}
            </ul>
            {state.error !== null && (
                <ErrorState
                    title="History could not be read"
                    message={errorMessage(state.error)}
                    onRetry={() => void load(state.started ? state.cursor : null, state.started)}
                />
            )}
            {state.started && state.cursor !== null && state.error === null && (
                <Button variant="secondary" className="w-full" disabled={state.loading} onClick={() => void load(state.cursor, true)}>
                    Load more
                </Button>
            )}
            {state.started && state.cursor === null && state.items.length > 0 && (
                <p data-history="end" className="text-center text-[11px] text-pax-muted">
                    End of history
                </p>
            )}
        </section>
    );
}
