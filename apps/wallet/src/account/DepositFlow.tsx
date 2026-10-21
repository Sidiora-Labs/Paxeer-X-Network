'use client';

import { useEffect, useMemo, useRef, useState } from 'react';
import { ethers } from 'ethers';
import { PAXEER_CHAIN_ID, decodeCustodyAuthorization, decodeCustodyStatus, type Hex, type JoinedAsset } from '@paxeer/wallet';
import { Button, ErrorState, Skeleton, TextField } from '@/components/ui/primitives';
import { useWallet } from '@/wallet/WalletProvider';
import { useAccountClients } from './AccountProvider';
import { CUSTODY_PRECOMPILE, beneficiaryOf, depositTokenCalldata } from './custody';
import { units } from './format';
import { errorMessage, kernelGate, kernelStateGate, useAccountDocument, useAsync, useKernelState } from './hooks';
import { KernelNotice } from './KernelNotice';
import { TransactionLadder } from './StatusLadder';

type RetainedDeposit = {
    readonly version: 2; readonly owner: string; readonly account: Hex; readonly chainId: number;
    readonly custody: Hex; readonly signature: Hex | null; readonly hash: Hex | null;
    readonly state: 'review' | 'signing' | 'signed' | 'submitting' | 'pending' | 'confirmed' | 'reverted';
};
type Progress =
    | { readonly step: 'idle' }
    | { readonly step: 'review'; readonly record: RetainedDeposit }
    | { readonly step: 'signing'; readonly record: RetainedDeposit }
    | { readonly step: 'pending'; readonly record: RetainedDeposit; readonly reason?: string }
    | { readonly step: 'settled'; readonly record: RetainedDeposit }
    | { readonly step: 'refused'; readonly reason: string; readonly blocked?: boolean };

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
    const [restoring, setRestoring] = useState(true);
    const storageKey = `paxeer:custody:v2:${wallet.identity?.id ?? 'no-session'}:${PAXEER_CHAIN_ID}:${address.toLowerCase()}`;
    const session = useRef(storageKey); session.current = storageKey;
    const inFlight = useRef(false);
    const persist = (record: RetainedDeposit) => { window.localStorage.setItem(storageKey, JSON.stringify(record)); };

    useEffect(() => {
        let disposed = false;
        setRestoring(true); setProgress({ step: 'idle' });
        void (async () => {
            try {
                const raw = window.localStorage.getItem(storageKey);
                if (raw === null) return;
                const record = JSON.parse(raw) as RetainedDeposit;
                if (record.version !== 2 || record.owner !== wallet.identity?.id || record.account.toLowerCase() !== address.toLowerCase() || record.chainId !== PAXEER_CHAIN_ID ||
                    !['review','signing','signed','submitting','pending','confirmed','reverted'].includes(record.state) ||
                    (record.hash !== null && !/^0x[0-9a-fA-F]{64}$/.test(record.hash))) throw new Error('Retained custody state is invalid');
                const authorization = decodeCustodyAuthorization(record.custody);
                if (authorization.account.toLowerCase() !== address.toLowerCase() || authorization.chainId !== BigInt(PAXEER_CHAIN_ID)) throw new Error('Retained deposit belongs to another account or network');
                if (record.signature !== null) {
                    if (!wallet.wallet) throw new Error('Reconnect the wallet to recover this deposit');
                    await wallet.wallet.provider.request({ method: 'paxeer_restoreCustody', params: [{ custody: record.custody, signature: record.signature }] });
                }
                if (disposed) return;
                setProgress(record.signature === null ? { step:'review',record } : { step:'pending',record });
            } catch (error) { if (!disposed) setProgress({ step:'refused',reason:errorMessage(error),blocked:true }); }
            finally { if (!disposed) setRestoring(false); }
        })();
        return () => { disposed = true; };
    }, [storageKey, address, wallet.identity?.id, wallet.wallet]);

    useEffect(() => {
        if (progress.step !== 'pending' || !wallet.wallet) return;
        let disposed = false; let running = false;
        const record = progress.record; const provider = wallet.wallet.provider;
        const poll = async () => {
            if (running) return; running = true;
            try {
                const raw = await provider.request({ method:'paxeer_custodyStatus',params:[{custody:record.custody}] });
                const status = decodeCustodyStatus(raw, ethers.keccak256(record.custody) as Hex);
                if (disposed || session.current !== storageKey) return;
                const next:RetainedDeposit = {...record,hash:status.tx_hash,state:status.status};
                window.localStorage.setItem(storageKey,JSON.stringify(next));
                setProgress(status.status === 'pending' ? {step:'pending',record:next} : {step:'settled',record:next});
            } catch (error) {
                if (!disposed && session.current === storageKey) setProgress(current => current.step === 'pending' && current.record.custody === record.custody ? {...current,reason:`Awaiting receipt evidence. ${errorMessage(error)}`} : current);
            } finally { running = false; }
        };
        void poll(); const timer = window.setInterval(() => void poll(), 5000);
        return () => { disposed = true; window.clearInterval(timer); };
    }, [progress.step, progress.step === 'pending' ? progress.record.custody : null, storageKey, wallet.wallet]);

    const options = useMemo(() => (assets.status === 'ready' ? candidates(assets.value.assets) : []), [assets]);
    const chosen = options.find((option) => option.entry.asset_id === assetId) ?? options[0] ?? null;
    const value = chosen ? parseAmount(amount, chosen.decimals) : null;

    const gate = kernelGate(kernelState);
    const reasons: string[] = [];
    if (!gate.open) reasons.push(gate.reason);
    if (progress.step === 'refused' && progress.blocked) reasons.push('Retained deposit state must be recovered before preparing another deposit');
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
    const busy = restoring || progress.step === 'signing' || progress.step === 'review' || progress.step === 'pending' || progress.step === 'settled';
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
            if (!wallet.identity) throw new Error('A signed-in owner session is required');
            const data = depositTokenCalldata(pointer, value, beneficiaryOf(layerxAccount));
            const bytes = await wallet.wallet.provider.request({method:'paxeer_prepareCustody',params:[{account:address,chainId:PAXEER_CHAIN_ID,to:CUSTODY_PRECOMPILE,value:0n,data}]});
            if (typeof bytes !== 'string') throw new Error('The wallet did not return a custody authorization');
            decodeCustodyAuthorization(bytes as Hex);
            const record:RetainedDeposit = {version:2,owner:wallet.identity.id,account:address,chainId:PAXEER_CHAIN_ID,custody:bytes as Hex,signature:null,hash:null,state:'review'};
            persist(record);
            if (session.current === storageKey) setProgress({step:'review',record});
        } catch (error) { setProgress({ step:'refused',reason:errorMessage(error) }); }
    }

    async function sendApproved(record: RetainedDeposit) {
        if (!wallet.wallet || record.signature === null) throw new Error('The approved custody proof is missing');
        const call = decodeCustodyAuthorization(record.custody);
        await wallet.wallet.provider.request({method:'paxeer_restoreCustody',params:[{custody:record.custody,signature:record.signature}]});
        const pending:RetainedDeposit = {...record,state:'submitting'}; persist(pending);
        setProgress({step:'pending',record:pending});
        try {
            const hash = await wallet.sendTransaction({to:call.to,data:call.data,value:call.value,chainId:Number(call.chainId),nonce:Number(call.nonce),gas:call.gas,maxFeePerGas:call.maxFeePerGas,maxPriorityFeePerGas:call.maxPriorityFeePerGas});
            const next:RetainedDeposit = {...pending,hash,state:'pending'}; persist(next);
            if (session.current === storageKey) setProgress({step:'pending',record:next});
        } catch (error) {
            if (session.current === storageKey) setProgress({step:'pending',record:pending,reason:`Submission outcome is not confirmed. ${errorMessage(error)}`});
        }
    }

    async function approve() {
        if (progress.step !== 'review' || !wallet.wallet || inFlight.current) return;
        inFlight.current = true; const record = progress.record;
        try {
            const signing:RetainedDeposit = {...record,state:'signing'}; persist(signing); setProgress({step:'signing',record:signing});
            const signature = await wallet.wallet.signCustody(record.custody);
            const signed:RetainedDeposit = {...record,signature,state:'signed'}; persist(signed);
            if (session.current === storageKey) await sendApproved(signed);
        } catch (error) { if (session.current === storageKey) setProgress({step:'refused',reason:errorMessage(error)}); }
        finally { inFlight.current = false; }
    }

    async function retryApproved() {
        if (progress.step !== 'pending' || inFlight.current) return;
        inFlight.current = true;
        try { await sendApproved(progress.record); }
        catch (error) { setProgress({...progress,reason:errorMessage(error)}); }
        finally { inFlight.current = false; }
    }
    const authorization = 'record' in progress ? decodeCustodyAuthorization(progress.record.custody) : null;
    const disclosedCall = authorization ? new ethers.Interface(['function deposit(bytes32 beneficiary) payable','function depositToken(address pointer,uint256 amount,bytes32 beneficiary)']).parseTransaction({data:authorization.data,value:authorization.value}) : null;
    const disclosedPointer = disclosedCall?.name === 'depositToken' ? String(disclosedCall.args[0]) : null;
    const disclosedAsset = options.find(item => item.entry.paxeer?.pointer.toLowerCase() === disclosedPointer?.toLowerCase());
    const disclosedAmount = disclosedCall?.name === 'depositToken'
        ? disclosedAsset ? `${units(String(disclosedCall.args[1]),disclosedAsset.decimals)} ${disclosedAsset.symbol}` : `${String(disclosedCall.args[1])} token base units (${disclosedPointer})`
        : authorization ? `${ethers.formatEther(authorization.value)} PAX` : '';
    const disclosedBeneficiary = disclosedCall ? String(disclosedCall.args[disclosedCall.name === 'depositToken' ? 2 : 0]) : '';

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
                        disabled={busy}
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
                disabled={busy}
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
                {restoring ? 'Restoring deposit' : 'Prepare custody deposit'}
            </Button>
            {progress.step === 'refused' && (
                <p role="alert" data-deposit="refused" className="text-xs text-pax-error">
                    {progress.reason}
                </p>
            )}
            {progress.step === 'review' && authorization && (
                <div className="space-y-2" data-deposit="review">
                    <p className="text-xs text-pax-muted">Approve this deposit on network {authorization.chainId.toString()}. Maximum network fee: {ethers.formatEther(authorization.gas * authorization.maxFeePerGas)} PAX. Approval expires {new Date(Number(authorization.deadline) * 1000).toLocaleTimeString()}.</p>
                    <p className="break-all text-xs text-pax-muted">Account {authorization.account}; destination {authorization.to}; nonce {authorization.nonce.toString()}.</p>
                    <p className="text-xs text-pax-muted">Deposit {disclosedAmount}</p>
                    <p className="break-all text-xs text-pax-muted">Beneficiary {disclosedBeneficiary}</p>
                    <Button name="approve-deposit" onClick={() => void approve()}>Approve and submit this deposit</Button>
                    <Button name="cancel-deposit" onClick={() => {window.localStorage.removeItem(storageKey);setProgress({step:'idle'});}}>Cancel approval</Button>
                </div>
            )}
            {progress.step === 'signing' && <p data-deposit="signing">Signing the approved custody authorization.</p>}
            {progress.step === 'pending' && (
                <div className="space-y-2" data-deposit="pending">
                    <p className="text-xs text-pax-muted">Pending receipt evidence. This deposit is not confirmed.</p>
                    {progress.reason && <p role="status" className="text-xs text-pax-muted">{progress.reason}</p>}
                    <Button name="retry-approved-deposit" onClick={() => void retryApproved()}>Resume the same approved deposit</Button>
                </div>
            )}
            {progress.step === 'settled' && <p data-deposit={progress.record.state}>{progress.record.state === 'confirmed' ? 'Deposit confirmed by its transaction receipt.' : 'The transaction receipt reports a reverted deposit.'}</p>}
            {'record' in progress && progress.record.signature && <p data-deposit="signature" className="break-all font-mono text-[11px] text-pax-muted">Custody authorization signed {progress.record.signature}</p>}
            {'record' in progress && progress.record.hash && <div className="space-y-2"><p data-deposit="hash" className="break-all font-mono text-[11px] text-pax-light">{progress.record.hash}</p><TransactionLadder hash={progress.record.hash} /></div>}
            {progress.step === 'settled' && <Button onClick={() => {window.localStorage.removeItem(storageKey);setProgress({step:'idle'});}}>Prepare another deposit</Button>}

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
