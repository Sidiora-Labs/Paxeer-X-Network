'use client';

import { useMemo, useState } from 'react';
import { parseUnits } from 'viem';
import { launchpad, type IntentLeg, type ModuleTransaction } from '@paxeer/wallet';
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
    const module = useMemo(() => (provider ? launchpad(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const state = useModuleSend(module, provider, address);
    const [action, setAction] = useState<LaunchpadAction>('buy');
    const [token, setToken] = useState('');
    const [amountIn, setAmountIn] = useState('');
    const [minOut, setMinOut] = useState('');
    const [deadlineMinutes, setDeadlineMinutes] = useState('10');
    const [name, setName] = useState('');
    const [symbol, setSymbol] = useState('');
    const [feeStrategy, setFeeStrategy] = useState('0');

    const built = useMemo(() => {
        if (!module || !address) return { value: null, error: null };
        return buildOrError<ModuleTransaction | null>(() => {
            switch (action) {
                case 'buy':
                case 'sell': {
                    if (!token || !amountIn || !minOut) return null;
                    const buying = action === 'buy';
                    const order = {
                        token,
                        amountIn: buying ? parseUnits(amountIn, 18) : BigInt(amountIn),
                        minOut: buying ? BigInt(minOut) : parseUnits(minOut, 18),
                        recipient: address,
                        deadline: BigInt(Math.floor(now() / 1000) + Number(deadlineMinutes) * 60),
                    };
                    return buying ? module.buy(order) : module.sell(order);
                }
                case 'create':
                    return name && symbol ? module.createMarket(name, symbol, Number(feeStrategy)) : null;
                case 'claim':
                    return token ? module.claimFees(token, address) : null;
            }
        });
    }, [module, address, action, token, amountIn, minOut, deadlineMinutes, name, symbol, feeStrategy, now]);

    return (
        <SurfaceFrame title="Launchpad" connected={module !== null}>
            <SegmentedControl label="Launchpad action" value={action} options={ACTIONS} onChange={setAction} />
            {action !== 'create' && (
                <TextField label="Token address" name="token" value={token} onChange={(e) => setToken(e.target.value)} />
            )}
            {(action === 'buy' || action === 'sell') && (
                <>
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
            <TransactionPreview transaction={built.value} />
            <FeeChoice provider={provider} address={address} transaction={built.value} selection={fee} sidRate={sidRate} legs={legs} />
            <SendButton label="Send to the launchpad" transaction={built.value} blocked={fee.blocked} state={state} />
            <SentResult state={state} />
        </SurfaceFrame>
    );
}
