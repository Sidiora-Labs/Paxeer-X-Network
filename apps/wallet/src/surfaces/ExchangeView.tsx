'use client';

import { useCallback, useMemo, useState } from 'react';
import { parseUnits } from 'viem';
import { exchange, type IntentLeg, type ModuleTransaction } from '@paxeer/wallet';
import { SegmentedControl, TextField } from '@/components/ui/primitives';
import { TOKENS } from '@/lib/constants';
import { FeeChoice } from './FeeChoice';
import { BuildError, SendButton, SentResult, SurfaceFrame, TransactionPreview, buildOrError } from './Surface';
import { useFeeSelection, useModuleSend, useSurfaceWallet } from './useSurface';

export type ExchangeAction = 'open' | 'close' | 'cancel' | 'deposit' | 'withdraw';

const ACTIONS: readonly { value: ExchangeAction; label: string }[] = [
    { value: 'open', label: 'Open' },
    { value: 'close', label: 'Close' },
    { value: 'cancel', label: 'Cancel' },
    { value: 'deposit', label: 'Deposit' },
    { value: 'withdraw', label: 'Withdraw' },
];

const SIDES: readonly { value: 'long' | 'short'; label: string }[] = [
    { value: 'long', label: 'Long' },
    { value: 'short', label: 'Short' },
];

const usdlToken = TOKENS.USDL;
if (!usdlToken?.address) throw new Error('the USDL collateral address is not configured');
export const USDL = { address: usdlToken.address, decimals: usdlToken.decimals };

export interface ExchangeViewProps {
    readonly sidRate: string | null;
    readonly legs?: readonly IntentLeg[];
}

interface Built {
    readonly tx: ModuleTransaction;
    readonly pull: bigint;
}

export function ExchangeView({ sidRate, legs }: ExchangeViewProps) {
    const { provider, address } = useSurfaceWallet();
    const surfaceModule = useMemo(() => (provider ? exchange(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const [action, setAction] = useState<ExchangeAction>('open');
    const [marketId, setMarketId] = useState('');
    const [side, setSide] = useState<'long' | 'short'>('long');
    const [collateral, setCollateral] = useState('');
    const [leverage, setLeverage] = useState('');
    const [id, setId] = useState('');
    const [amount, setAmount] = useState('');

    const built = useMemo(() => {
        if (!surfaceModule) return { value: null, error: null };
        return buildOrError<Built | null>(() => {
            switch (action) {
                case 'open': {
                    if (!marketId || !collateral || !leverage) return null;
                    const collateralAmount = parseUnits(collateral, USDL.decimals);
                    const tx = surfaceModule.openPosition({
                        marketId: BigInt(marketId),
                        collateralToken: USDL.address,
                        collateralAmount,
                        leverage: parseUnits(leverage, 18),
                        isLong: side === 'long',
                    });
                    return { tx, pull: collateralAmount };
                }
                case 'close':
                    return id ? { tx: surfaceModule.closePosition(BigInt(id)), pull: 0n } : null;
                case 'cancel':
                    return id ? { tx: surfaceModule.cancelOrder(BigInt(id)), pull: 0n } : null;
                case 'deposit': {
                    if (!amount) return null;
                    const pull = parseUnits(amount, USDL.decimals);
                    return { tx: surfaceModule.deposit(USDL.address, pull), pull };
                }
                case 'withdraw':
                    return amount ? { tx: surfaceModule.withdraw(USDL.address, parseUnits(amount, USDL.decimals)), pull: 0n } : null;
            }
        });
    }, [surfaceModule, action, marketId, side, collateral, leverage, id, amount]);
    const transaction = built.value?.tx ?? null;
    const pull = built.value?.pull ?? 0n;
    const approval = useCallback(
        async (owner: string) => (surfaceModule && pull > 0n ? surfaceModule.approval(owner, USDL.address, pull) : undefined),
        [surfaceModule, pull],
    );
    const state = useModuleSend(surfaceModule, provider, address, fee, approval);

    return (
        <SurfaceFrame title="Exchange" connected={surfaceModule !== null}>
            <SegmentedControl label="Exchange action" value={action} options={ACTIONS} onChange={setAction} />
            {action === 'open' && (
                <>
                    <TextField label="Market id" name="marketId" value={marketId} onChange={(e) => setMarketId(e.target.value)} />
                    <SegmentedControl label="Side" value={side} options={SIDES} onChange={setSide} />
                    <TextField label="Collateral (USDL)" name="collateral" value={collateral} onChange={(e) => setCollateral(e.target.value)} />
                    <TextField label="Leverage (x)" name="leverage" value={leverage} onChange={(e) => setLeverage(e.target.value)} />
                </>
            )}
            {(action === 'close' || action === 'cancel') && (
                <TextField
                    label={action === 'close' ? 'Position id' : 'Order id'}
                    name="id"
                    value={id}
                    onChange={(e) => setId(e.target.value)}
                />
            )}
            {(action === 'deposit' || action === 'withdraw') && (
                <TextField label="Amount (USDL)" name="amount" value={amount} onChange={(e) => setAmount(e.target.value)} />
            )}
            {pull > 0n && (
                <p data-role="approve-step" className="text-xs text-pax-muted">
                    {state.approving
                        ? 'Approving the exact USDL amount for the perps venue'
                        : 'If the perps venue allowance is short, one approve for the exact USDL amount is sent first'}
                </p>
            )}
            <BuildError error={built.error} />
            <TransactionPreview transaction={transaction} />
            <FeeChoice provider={provider} address={address} transaction={transaction} selection={fee} sidRate={sidRate} legs={legs} />
            <SendButton label="Send to the perps venue" transaction={transaction} blocked={fee.blocked} state={state} />
            <SentResult state={state} />
        </SurfaceFrame>
    );
}
