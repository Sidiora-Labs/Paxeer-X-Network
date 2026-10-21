'use client';

import { useMemo } from 'react';
import { kernelUnavailableFrom, type AccountBalances, type CompletionAsset, type JoinedAsset } from '@paxeer/wallet';
import { AmountText, ErrorState, Skeleton } from '@/components/ui/primitives';
import { completionAssets, units } from './format';
import { errorMessage, kernelStateGate, useAsync } from './hooks';
import { useAccountClients } from './AccountProvider';
import { KernelNotice } from './KernelNotice';

function metadata(entry: JoinedAsset | undefined): { symbol: string; decimals: number | null } {
    const symbol = typeof entry?.layerx.symbol === 'string' ? entry.layerx.symbol : null;
    const decimals = typeof entry?.layerx.decimals === 'number' ? entry.layerx.decimals : null;
    return { symbol: symbol ?? 'Unknown asset', decimals };
}

function completedLabel(asset: CompletionAsset): string {
    if (asset.kind === 'native') return asset.symbol;
    return asset.symbol ?? asset.address;
}

function Balances({ balances }: { balances: AccountBalances }) {
    const entries = new Map(balances.asset_map.assets.map((entry) => [entry.asset_id, entry]));
    return (
        <>
            <ul aria-label="Joined balances" className="list-separated">
                {balances.balances.map((row) => {
                    const { symbol, decimals } = metadata(entries.get(row.asset_id));
                    const layerx = typeof row.layerx?.balance === 'string' ? row.layerx.balance : null;
                    return (
                        <li key={row.asset_id} data-asset={row.asset_id} className="flex items-start justify-between gap-3 py-3">
                            <div>
                                <p className="text-sm font-semibold">{symbol}</p>
                                <p className="text-[11px] text-pax-muted">{row.denom ?? 'No denomination'}</p>
                            </div>
                            <div className="space-y-1 text-right text-xs">
                                <p data-side="paxeer">
                                    Paxeer{' '}
                                    {row.paxeer ? (
                                        <AmountText label={`${symbol} on Paxeer`} value={units(row.paxeer.amount, decimals)} symbol={symbol} />
                                    ) : (
                                        <span className="text-pax-muted">no balance</span>
                                    )}
                                </p>
                                <p data-side="layerx">
                                    LayerX{' '}
                                    {layerx !== null ? (
                                        <AmountText label={`${symbol} on LayerX`} value={units(layerx, decimals)} symbol={symbol} />
                                    ) : (
                                        <span className="text-pax-muted">no balance</span>
                                    )}
                                </p>
                            </div>
                        </li>
                    );
                })}
                {balances.completed.map((completed) => {
                    const label = completedLabel(completed.asset);
                    const key = completed.asset.kind === 'native' ? `native:${label}` : completed.asset.address.toLowerCase();
                    return (
                        <li key={key} data-completed={key} className="flex items-start justify-between gap-3 py-3">
                            <div>
                                <p className="text-sm font-semibold">{label}</p>
                                <p className="text-[11px] text-pax-muted">
                                    {completed.source === 'eth_getBalance' ? 'Chain balance' : 'Token contract balance'}
                                </p>
                            </div>
                            <div className="text-right text-xs">
                                {completed.amount !== null ? (
                                    <AmountText label={`${label} balance`} value={units(completed.amount, completed.asset.decimals)} symbol={label} />
                                ) : (
                                    <span className="text-pax-error">{completed.error?.message ?? 'unreadable'}</span>
                                )}
                            </div>
                        </li>
                    );
                })}
            </ul>
            <p data-join-limit={balances.joined_limit} className="text-[11px] text-pax-muted">
                {balances.join_limit_reached
                    ? `The endpoint joins at most ${balances.joined_limit} assets and that limit is reached; assets beyond it are read from the chain.`
                    : `${balances.asset_map.assets.length} of at most ${balances.joined_limit} joined assets.`}
            </p>
        </>
    );
}

export function AssetList({ account, assets }: { account: string; assets?: readonly CompletionAsset[] }) {
    const { endpoint } = useAccountClients();
    const completion = useMemo(() => assets ?? completionAssets(), [assets]);
    const balances = useAsync(() => endpoint.getBalances(account, completion), `balances:${account.toLowerCase()}`);
    const unavailable = balances.status === 'error' ? kernelUnavailableFrom(balances.error) : null;

    return (
        <section aria-label="Assets" className="space-y-3 rounded-[20px] bg-pax-surface p-4">
            <h2 className="text-sm font-bold">Assets</h2>
            {balances.status === 'loading' && <Skeleton className="h-24 w-full" />}
            {balances.status === 'ready' && <Balances balances={balances.value} />}
            {unavailable && <KernelNotice gate={kernelStateGate(unavailable)} />}
            {balances.status === 'error' && !unavailable && (
                <ErrorState title="Balances could not be read" message={errorMessage(balances.error)} onRetry={balances.reload} />
            )}
        </section>
    );
}
