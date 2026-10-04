'use client';

import { useMemo, useState } from 'react';
import { parseUnits } from 'viem';
import { exchange, type IntentLeg, type ModuleTransaction } from '@paxeer/wallet';
import { SegmentedControl, TextField } from '@/components/ui/primitives';
import { FeeChoice } from './FeeChoice';
import { BuildError, SendButton, SentResult, SurfaceFrame, TransactionPreview, buildOrError } from './Surface';
import { useFeeSelection, useModuleSend, useSurfaceWallet } from './useSurface';

export type ExchangeAction = 'order' | 'cancel' | 'deposit' | 'withdraw';

const ACTIONS: readonly { value: ExchangeAction; label: string }[] = [
    { value: 'order', label: 'Order' },
    { value: 'cancel', label: 'Cancel' },
    { value: 'deposit', label: 'Deposit' },
    { value: 'withdraw', label: 'Withdraw' },
];

const SIDES: readonly { value: '0' | '1'; label: string }[] = [
    { value: '0', label: 'Buy' },
    { value: '1', label: 'Sell' },
];

export interface ExchangeViewProps {
    readonly sidRate: string | null;
    readonly legs?: readonly IntentLeg[];
}

export function ExchangeView({ sidRate, legs }: ExchangeViewProps) {
    const { provider, address } = useSurfaceWallet();
    const surfaceModule = useMemo(() => (provider ? exchange(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const state = useModuleSend(surfaceModule, provider, address, fee);
    const [action, setAction] = useState<ExchangeAction>('order');
    const [marketId, setMarketId] = useState('');
    const [side, setSide] = useState<'0' | '1'>('0');
    const [price, setPrice] = useState('');
    const [quantity, setQuantity] = useState('');
    const [timeInForce, setTimeInForce] = useState('0');
    const [orderId, setOrderId] = useState('');
    const [account, setAccount] = useState('');
    const [assetId, setAssetId] = useState('');
    const [amount, setAmount] = useState('');

    const built = useMemo(() => {
        if (!surfaceModule) return { value: null, error: null };
        return buildOrError<ModuleTransaction | null>(() => {
            switch (action) {
                case 'order':
                    if (!marketId || !price || !quantity) return null;
                    return surfaceModule.placeOrder({
                        marketId,
                        side: Number(side),
                        price: parseUnits(price, 18),
                        quantity: BigInt(quantity),
                        timeInForce: Number(timeInForce),
                    });
                case 'cancel':
                    return orderId ? surfaceModule.cancelOrder(orderId) : null;
                case 'deposit':
                    return account && amount ? surfaceModule.depositMargin(account, parseUnits(amount, 18)) : null;
                case 'withdraw':
                    return account && assetId && amount ? surfaceModule.withdrawMargin(account, assetId, BigInt(amount)) : null;
            }
        });
    }, [surfaceModule, action, marketId, side, price, quantity, timeInForce, orderId, account, assetId, amount]);

    return (
        <SurfaceFrame title="Exchange" connected={surfaceModule !== null}>
            <SegmentedControl label="Exchange action" value={action} options={ACTIONS} onChange={setAction} />
            {action === 'order' && (
                <>
                    <TextField label="Market id" name="marketId" value={marketId} onChange={(e) => setMarketId(e.target.value)} />
                    <SegmentedControl label="Side" value={side} options={SIDES} onChange={setSide} />
                    <TextField label="Price (PAX)" name="price" value={price} onChange={(e) => setPrice(e.target.value)} />
                    <TextField label="Quantity (base units)" name="quantity" value={quantity} onChange={(e) => setQuantity(e.target.value)} />
                    <TextField label="Time in force" name="timeInForce" value={timeInForce} onChange={(e) => setTimeInForce(e.target.value)} />
                </>
            )}
            {action === 'cancel' && (
                <TextField label="Order id" name="orderId" value={orderId} onChange={(e) => setOrderId(e.target.value)} />
            )}
            {(action === 'deposit' || action === 'withdraw') && (
                <TextField label="Margin account" name="account" value={account} onChange={(e) => setAccount(e.target.value)} />
            )}
            {action === 'withdraw' && (
                <TextField label="Asset id" name="assetId" value={assetId} onChange={(e) => setAssetId(e.target.value)} />
            )}
            {(action === 'deposit' || action === 'withdraw') && (
                <TextField
                    label={action === 'deposit' ? 'Amount (PAX)' : 'Amount (base units)'}
                    name="amount"
                    value={amount}
                    onChange={(e) => setAmount(e.target.value)}
                />
            )}
            <BuildError error={built.error} />
            <TransactionPreview transaction={built.value} />
            <FeeChoice provider={provider} address={address} transaction={built.value} selection={fee} sidRate={sidRate} legs={legs} />
            <SendButton label="Send to the exchange" transaction={built.value} blocked={fee.blocked} state={state} />
            <SentResult state={state} />
        </SurfaceFrame>
    );
}
