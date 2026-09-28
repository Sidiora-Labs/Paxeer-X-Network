'use client';

import { useMemo, useState } from 'react';
import { ethers } from 'ethers';
import { PAXEER_CHAIN_ID, type Hex, type JoinedAsset } from '@paxeer/wallet';
import { Button, ErrorState, Skeleton, TextField } from '@/components/ui/primitives';
import { useWallet } from '@/wallet/WalletProvider';
import { useAccountClients } from './AccountProvider';
import { CUSTODY_PRECOMPILE, beneficiaryOf, custodyHandoff, depositTokenCalldata } from './custody';
import { units } from './format';
import { errorMessage, kernelGate, kernelStateGate, useAccountDocument, useAsync, useKernelState } from './hooks';
import { KernelNotice } from './KernelNotice';
import { TransactionLadder } from './StatusLadder';

type Progress =
    | { readonly step: 'idle' }
    | { readonly step: 'signing' }
    | { readonly step: 'sending'; readonly signature: Hex }
    | { readonly step: 'sent'; readonly signature: Hex; readonly hash: Hex }
    | { readonly step: 'refused'; readonly reason: string };

interface Candidate {
    readonly entry: JoinedAsset;
    readonly symbol: string;
    readonly decimals: number;
}

function candidates(assets: readonly JoinedAsset[]): Candidate[] {
    return assets.flatMap((entry) => {
        const custody = entry.paxeer;
        if (!custody || !custody.enabled || custody.paused) return [];
        const decimals = entry.layerx.decimals;
        if (typeof decimals !== 'number') return [];
        const symbol = typeof entry.layerx.symbol === 'string' ? entry.layerx.symbol : custody.denom;
        return [{ entry, symbol, decimals }];
    });
}

function parseAmount(text: string, decimals: number): bigint | null {
    if (!/^\d+(\.\d+)?$/.test(text.trim())) return null;
    try {
        const value = ethers.parseUnits(text.trim(), decimals);
        return value > 0n ? value : null;
    } catch {
        return null;
    }
}

function DepositForm({ address }: { address: Hex }) {
    const wallet = useWallet();
    const { endpoint, kernel } = useAccountClients();
    const kernelState = useKernelState();
    const document = useAccountDocument(address);
    const assets = useAsync(() => endpoint.listAssets(), 'asset-map');
    const [assetId, setAssetId] = useState<string | null>(null);
    const [amount, setAmount] = useState('');
    const [progress, setProgress] = useState<Progress>({ step: 'idle' });

    const options = useMemo(() => (assets.status === 'ready' ? candidates(assets.value.assets) : []), [assets]);
    const chosen = options.find((option) => option.entry.asset_id === assetId) ?? options[0] ?? null;
    const value = chosen ? parseAmount(amount, chosen.decimals) : null;

    const gate = kernelGate(kernelState);
    const reasons: string[] = [];
    if (!gate.open) reasons.push(gate.reason);
    if (wallet.mode !== 'embedded') reasons.push('Custody deposits are signed by the embedded wallet');
    if (document.status === 'ready' && (!document.value.bound || !document.value.layerx_account)) {
        reasons.push('The account is not bound to its LayerX identity');
    }
    if (assets.status === 'ready' && options.length === 0) reasons.push('No asset is open for custody deposits');
    if (chosen && amount.trim() !== '' && value === null) reasons.push('Enter a positive amount');
    if (chosen && value !== null) {
        const custody = chosen.entry.paxeer;
        if (custody && value < BigInt(custody.minimum_deposit)) {
            reasons.push(`The minimum deposit is ${units(custody.minimum_deposit, chosen.decimals)} ${chosen.symbol}`);
        }
        if (custody && BigInt(custody.custodied) + value > BigInt(custody.custody_cap)) {
            reasons.push('The deposit would exceed the custody cap');
        }
    }
    const busy = progress.step === 'signing' || progress.step === 'sending';
    const ready = document.status === 'ready' && chosen !== null && value !== null && reasons.length === 0 && !busy;

    async function submit() {
        if (!ready || document.status !== 'ready' || !chosen || value === null || !wallet.wallet) return;
        const current = await kernel.current().catch(() => null);
        if (current === null) {
            setProgress({ step: 'refused', reason: 'The endpoint did not report the LayerX kernel state' });
            return;
        }
        const currentGate = kernelStateGate(current);
        if (!currentGate.open) {
            setProgress({ step: 'refused', reason: currentGate.reason });
            return;
        }
        const pointer = chosen.entry.paxeer?.pointer;
        const layerxAccount = document.value.layerx_account;
        if (!pointer || !layerxAccount) return;
        try {
            const data = depositTokenCalldata(pointer, value, beneficiaryOf(layerxAccount));
            setProgress({ step: 'signing' });
            const signature = await wallet.wallet.signCustody(
                custodyHandoff({ chainId: PAXEER_CHAIN_ID, to: CUSTODY_PRECOMPILE, value: 0n, data }),
            );
            setProgress({ step: 'sending', signature });
            const hash = await wallet.sendTransaction({ to: CUSTODY_PRECOMPILE, data, value: 0n, chainId: PAXEER_CHAIN_ID });
            setProgress({ step: 'sent', signature, hash });
        } catch (error) {
            setProgress({ step: 'refused', reason: errorMessage(error) });
        }
    }

    return (
        <div className="space-y-3">
            <KernelNotice gate={gate} />
            {(assets.status === 'loading' || document.status === 'loading') && <Skeleton className="h-10 w-full" />}
            {assets.status === 'error' && (
                <ErrorState title="The custody assets could not be read" message={errorMessage(assets.error)} onRetry={assets.reload} />
            )}
            {document.status === 'error' && (
                <ErrorState title="The account could not be resolved" message={errorMessage(document.error)} onRetry={document.reload} />
            )}
            {options.length > 0 && (
                <label className="block space-y-2">
                    <span className="block text-sm font-semibold text-pax-light">Asset</span>
                    <select
                        name="asset"
                        value={chosen?.entry.asset_id ?? ''}
                        onChange={(event) => setAssetId(event.target.value)}
                        className="min-h-[var(--touch-target)] w-full rounded-xl bg-[var(--color-surface-control)] px-3 py-2 text-base text-pax-light"
                    >
                        {options.map((option) => (
                            <option key={option.entry.asset_id} value={option.entry.asset_id}>
                                {option.symbol}
                            </option>
                        ))}
                    </select>
                </label>
            )}
            <TextField
                label="Amount"
                name="amount"
                inputMode="decimal"
                value={amount}
                onChange={(event) => setAmount(event.target.value)}
                hint={chosen && document.status === 'ready' ? `Credited to ${document.value.layerx_did ?? 'the main account'}` : undefined}
            />
            {reasons.length > 0 && (
                <ul aria-label="Deposit unavailable" className="space-y-1 text-xs text-pax-muted">
                    {reasons.map((reason) => (
                        <li key={reason} data-reason>
                            {reason}
                        </li>
                    ))}
                </ul>
            )}
            <Button className="w-full" name="deposit" disabled={!ready} onClick={() => void submit()}>
                {busy ? 'Depositing' : 'Deposit into custody'}
            </Button>
            {progress.step === 'refused' && (
                <p role="alert" data-deposit="refused" className="text-xs text-pax-error">
                    {progress.reason}
                </p>
            )}
            {(progress.step === 'sending' || progress.step === 'sent') && (
                <p data-deposit="signature" className="break-all font-mono text-[11px] text-pax-muted">
                    Custody hand-off signed {progress.signature}
                </p>
            )}
            {progress.step === 'sent' && (
                <div className="space-y-2">
                    <p data-deposit="hash" className="break-all font-mono text-[11px] text-pax-light">
                        {progress.hash}
                    </p>
                    <TransactionLadder hash={progress.hash} />
                </div>
            )}
        </div>
    );
}

export function DepositFlow() {
    const wallet = useWallet();
    return (
        <section aria-label="Custody deposit" className="space-y-3 rounded-[20px] bg-pax-surface p-4">
            <h2 className="text-sm font-bold">Deposit into custody</h2>
            {wallet.status === 'ready' && wallet.address ? (
                <DepositForm address={wallet.address} />
            ) : (
                <p className="text-xs text-pax-muted">Connect a wallet to deposit.</p>
            )}
        </section>
    );
}
