'use client';

import { useCallback, useMemo, useState } from 'react';
import { parseUnits } from 'viem';
import { launchpad, type IntentLeg, type LaunchpadSpend, type ModuleTransaction } from '@paxeer/wallet';
import { SegmentedControl, TextField } from '@/components/ui/primitives';
import { FeeChoice } from './FeeChoice';
import { BuildError, SendButton, SentResult, SurfaceFrame, TransactionPreview, buildOrError } from './Surface';
import { useFeeSelection, useModuleSend, useSurfaceWallet } from './useSurface';

export type LaunchpadAction = 'buy' | 'sell' | 'create' | 'claim';

const ACTIONS: readonly { value: LaunchpadAction; label: string }[] = [
    { value: 'buy', label: 'Buy' },
    { value: 'sell', label: 'Sell' },
    { value: 'create', label: 'Create' },
    { value: 'claim', label: 'Claim fees' },
];

export interface LaunchpadViewProps {
    readonly sidRate: string | null;
    readonly legs?: readonly IntentLeg[];
    readonly now?: () => number;
}

export function LaunchpadView({ sidRate, legs, now = Date.now }: LaunchpadViewProps) {
    const { provider, address } = useSurfaceWallet();
    const surfaceModule = useMemo(() => (provider ? launchpad(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const [action, setAction] = useState<LaunchpadAction>('buy');
    const [pool, setPool] = useState('');
    const [nftId, setNftId] = useState('');
    const [amountIn, setAmountIn] = useState('');
    const [minOut, setMinOut] = useState('');
    const [deadlineMinutes, setDeadlineMinutes] = useState('10');
    const [name, setName] = useState('');
    const [symbol, setSymbol] = useState('');
    const [feeStrategy, setFeeStrategy] = useState('0');

    const built = useMemo(() => {
        if (!surfaceModule || !address) return { value: null, error: null };
        return buildOrError<{ tx: ModuleTransaction; spend?: LaunchpadSpend } | null>(() => {
            switch (action) {
                case 'buy':
                case 'sell': {
                    if (!pool || !amountIn || !minOut) return null;
                    const buying = action === 'buy';
                    const order = {
                        pool,
                        amountIn: buying ? parseUnits(amountIn, 18) : BigInt(amountIn),
                        minOut: buying ? BigInt(minOut) : parseUnits(minOut, 18),
                        deadline: BigInt(Math.floor(now() / 1000) + Number(deadlineMinutes) * 60),
                    };
                    return buying ? { tx: surfaceModule.buy(order), spend: { kind: 'buy', order } } : { tx: surfaceModule.sell(order), spend: { kind: 'sell', order } };
                }
                case 'create':
                    return name && symbol ? { tx: surfaceModule.createMarket(name, symbol, Number(feeStrategy)), spend: { kind: 'create' } } : null;
                case 'claim':
                    return nftId ? { tx: surfaceModule.claimFees(BigInt(nftId)) } : null;
            }
        });
    }, [surfaceModule, address, action, pool, nftId, amountIn, minOut, deadlineMinutes, name, symbol, feeStrategy, now]);
    const transaction = built.value?.tx ?? null;
    const spend = built.value?.spend;
    const approval = useCallback(
        async (owner: string) => (surfaceModule && spend ? surfaceModule.approval(owner, spend) : undefined),
        [surfaceModule, spend],
    );
    const state = useModuleSend(surfaceModule, provider, address, fee, approval);

    return (
        <SurfaceFrame title="Launchpad" connected={surfaceModule !== null}>
            <SegmentedControl label="Launchpad action" value={action} options={ACTIONS} onChange={setAction} />
            {action === 'claim' && (
                <TextField label="Fee NFT id" name="nftId" value={nftId} onChange={(e) => setNftId(e.target.value)} />
            )}
            {(action === 'buy' || action === 'sell') && (
                <>
                    <TextField label="Pool address" name="pool" value={pool} onChange={(e) => setPool(e.target.value)} />
                    <TextField
                        label={action === 'buy' ? 'Pay (PAX)' : 'Sell (token base units)'}
                        name="amountIn"
                        value={amountIn}
                        onChange={(e) => setAmountIn(e.target.value)}
                    />
                    <TextField
                        label={action === 'buy' ? 'Receive at least (token base units)' : 'Receive at least (PAX)'}
                        name="minOut"
                        value={minOut}
                        onChange={(e) => setMinOut(e.target.value)}
                    />
                    <TextField
                        label="Deadline (minutes)"
                        name="deadlineMinutes"
                        value={deadlineMinutes}
                        onChange={(e) => setDeadlineMinutes(e.target.value)}
                    />
                </>
            )}
            {action === 'create' && (
                <>
                    <TextField label="Name" name="name" value={name} onChange={(e) => setName(e.target.value)} />
                    <TextField label="Symbol" name="symbol" value={symbol} onChange={(e) => setSymbol(e.target.value)} />
                    <TextField label="Fee strategy" name="feeStrategy" value={feeStrategy} onChange={(e) => setFeeStrategy(e.target.value)} />
                </>
            )}
            <BuildError error={built.error} />
            <TransactionPreview transaction={transaction} />
            <FeeChoice provider={provider} address={address} transaction={transaction} selection={fee} sidRate={sidRate} legs={legs} />
            {spend && (
                <p data-role="approve-step" className="text-xs text-pax-muted">
                    {state.approving
                        ? 'Approving the exact amount for the launchpad router'
                        : 'If the launchpad router allowance is short, one approve for the exact amount is sent first'}
                </p>
            )}
            <SendButton label="Send to the launchpad" transaction={transaction} blocked={fee.blocked} state={state} />
            <SentResult state={state} />
        </SurfaceFrame>
    );
}
