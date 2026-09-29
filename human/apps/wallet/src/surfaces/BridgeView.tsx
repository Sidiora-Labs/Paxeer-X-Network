'use client';

import { useMemo, useState } from 'react';
import { parseUnits } from 'viem';
import { bridge, type IntentLeg, type ModuleTransaction } from '@paxeer/wallet';
import { TextField } from '@/components/ui/primitives';
import { FeeChoice } from './FeeChoice';
import { BuildError, SendButton, SentResult, SurfaceFrame, TransactionPreview, buildOrError } from './Surface';
import { useFeeSelection, useModuleSend, useSurfaceWallet } from './useSurface';

export interface BridgeViewProps {
    readonly sidRate: string | null;
    readonly legs?: readonly IntentLeg[];
}

export function BridgeView({ sidRate, legs }: BridgeViewProps) {
    const { provider, address } = useSurfaceWallet();
    const surfaceModule = useMemo(() => (provider ? bridge(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const state = useModuleSend(surfaceModule, provider, address);
    const [chain, setChain] = useState('');
    const [asset, setAsset] = useState('');
    const [decimals, setDecimals] = useState('18');
    const [amount, setAmount] = useState('');
    const [recipient, setRecipient] = useState('');

    const built = useMemo(() => {
        if (!surfaceModule) return { value: null, error: null };
        return buildOrError<ModuleTransaction | null>(() => {
            if (!chain || !asset || !amount || !recipient) return null;
            return surfaceModule.bridgeOut(BigInt(chain), asset, parseUnits(amount, Number(decimals)), recipient);
        });
    }, [surfaceModule, chain, asset, decimals, amount, recipient]);

    return (
        <SurfaceFrame title="Bridge" connected={surfaceModule !== null}>
            <TextField label="Destination chain id" name="chain" value={chain} onChange={(e) => setChain(e.target.value)} />
            <TextField label="Asset address" name="asset" value={asset} onChange={(e) => setAsset(e.target.value)} />
            <TextField label="Asset decimals" name="decimals" value={decimals} onChange={(e) => setDecimals(e.target.value)} />
            <TextField label="Amount" name="amount" value={amount} onChange={(e) => setAmount(e.target.value)} />
            <TextField label="Recipient" name="recipient" value={recipient} onChange={(e) => setRecipient(e.target.value)} />
            <BuildError error={built.error} />
            <TransactionPreview transaction={built.value} />
            <FeeChoice provider={provider} address={address} transaction={built.value} selection={fee} sidRate={sidRate} legs={legs} />
            <SendButton label="Bridge out" transaction={built.value} blocked={fee.blocked} state={state} />
            <SentResult state={state} />
        </SurfaceFrame>
    );
}
