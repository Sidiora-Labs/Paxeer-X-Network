'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    SIDIORA_FEE_DENOM,
    PAXEER_CHAIN_ID, gasStation, GasStationError,
    type GasStationModule, type SponsoredBatch, type SponsoredConsent,
    webData,
    type WalletCapsState,
    feeToken,
    type FeeChoiceId,
    type ModuleProvider,
    type ModuleTransaction,
} from '@paxeer/wallet';
import { useWallet } from '@/wallet/WalletProvider';

export interface SurfaceLog {
    readonly address: string;
    readonly topics: readonly string[];
    readonly data: string;
}

export interface SurfaceEvent {
    readonly event: string;
    readonly fields: Readonly<Record<string, bigint | boolean | string>>;
}

export interface SurfaceModule {
    readonly address: string;
    send(from: string, tx: ModuleTransaction): Promise<string>;
    decodeEvent(log: SurfaceLog): SurfaceEvent;
}

export type ReceiptStatus = 'pending' | 'confirmed' | 'reverted';

export interface SentTransaction {
    readonly hash: string;
    readonly status: ReceiptStatus;
    readonly events: readonly SurfaceEvent[];
}

export interface SurfaceWallet {
    readonly provider: ModuleProvider | null;
    readonly address: string | null;
}

export const RECEIPT_ATTEMPTS = 30;
export const RECEIPT_INTERVAL_MS = 1_000;

export function useSurfaceWallet(): SurfaceWallet {
    const { status, wallet, address } = useWallet();
    return useMemo(
        () => (status === 'ready' && wallet && address ? { provider: wallet.provider, address } : { provider: null, address: null }),
        [status, wallet, address],
    );
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function isLog(value: unknown): value is SurfaceLog {
    return (
        isRecord(value) &&
        typeof value.address === 'string' &&
        typeof value.data === 'string' &&
        Array.isArray(value.topics) &&
        value.topics.every((topic) => typeof topic === 'string')
    );
}

export function receiptEvents(receipt: unknown, surfaceModule: Pick<SurfaceModule, 'address' | 'decodeEvent'>): SentTransaction['events'] {
    if (!isRecord(receipt) || !Array.isArray(receipt.logs)) throw new Error('the receipt carries no logs');
    return receipt.logs
        .filter(isLog)
        .filter((log) => log.address.toLowerCase() === surfaceModule.address)
        .map((log) => surfaceModule.decodeEvent(log));
}

export function receiptStatus(receipt: unknown): ReceiptStatus {
    if (!isRecord(receipt)) return 'pending';
    return receipt.status === '0x1' ? 'confirmed' : 'reverted';
}

export async function waitForReceipt(provider: ModuleProvider, hash: string, attempts = RECEIPT_ATTEMPTS, intervalMs = RECEIPT_INTERVAL_MS): Promise<unknown> {
    for (let attempt = 0; attempt < attempts; attempt += 1) {
        const receipt = await provider.request({ method: 'eth_getTransactionReceipt', params: [hash] });
        if (receipt !== null && receipt !== undefined) return receipt;
        await new Promise((resolve) => setTimeout(resolve, intervalMs));
    }
    return null;
}

export function formatField(value: bigint | boolean | string): string {
    return typeof value === 'bigint' ? value.toString(10) : String(value);
}

export function errorMessage(error: unknown): string {
    return error instanceof Error && error.message ? error.message : 'the request failed';
}

export interface ModuleSend {
    readonly sending: boolean;
    readonly sent: SentTransaction | null;
    readonly error: string | null;
    readonly send: (tx: ModuleTransaction) => Promise<void>;
}

export function useModuleSend(surfaceModule: SurfaceModule | null, provider: ModuleProvider | null, address: string | null, fee?: FeeSelection): ModuleSend {
    const [sending, setSending] = useState(false);
    const [sent, setSent] = useState<SentTransaction | null>(null);
    const [error, setError] = useState<string | null>(null);
    const alive = useRef(true);
    useEffect(() => {
        alive.current = true;
        return () => {
            alive.current = false;
        };
    }, []);

    const send = useCallback(
        async (tx: ModuleTransaction) => {
            if (!surfaceModule || !provider || !address) {
                setError('connect a wallet first');
                return;
            }
            setSending(true);
            setError(null);
            setSent(null);
            try {
                const hash = fee ? await fee.submit(tx, () => surfaceModule.send(address, tx)) : await surfaceModule.send(address, tx);
                if (alive.current) setSent({ hash, status: 'pending', events: [] });
                const receipt = await waitForReceipt(provider, hash);
                if (!alive.current) return;
                if (receipt === null) {
                    setSent({ hash, status: 'pending', events: [] });
                    return;
                }
                setSent({ hash, status: receiptStatus(receipt), events: receiptEvents(receipt, surfaceModule) });
            } catch (cause) {
                if (alive.current) setError(errorMessage(cause));
            } finally {
                if (alive.current) setSending(false);
            }
        },
        [surfaceModule, provider, address, fee],
    );

    return { sending, sent, error, send };
}

export interface FeeSelection {
    readonly choice: FeeChoiceId;
    readonly setChoice: (choice: FeeChoiceId) => void;
    readonly feeDenom: string | null;
    readonly denomError: string | null;
    readonly updating: boolean;
    readonly blocked: string | null;
    readonly applyPreference: () => Promise<void>;
    readonly sponsored: SponsoredSelection;
    readonly prepare: (tx: ModuleTransaction, maximum: bigint, gasCost: bigint) => Promise<void>;
    readonly approve: () => void;
    readonly cancel: () => void;
    readonly recover: () => Promise<void>;
    readonly submit: (tx: ModuleTransaction, direct: () => Promise<string>) => Promise<string>;
}

export const SPONSORED_UNAVAILABLE = 'the gas station quote service is not configured for this app';

export function feeBlocked(choice: FeeChoiceId, feeDenom: string | null): string | null {
    if (feeDenom === null) return 'the current fee token is not known yet';
    if (choice === 'sid_sponsored') return null;
    if (choice === 'sid_native' && feeDenom !== SIDIORA_FEE_DENOM) return `set ${SIDIORA_FEE_DENOM} as the fee token first`;
    if (choice === 'pax_gas' && feeDenom !== '') return 'clear the fee token preference first';
    return null;
}

export interface SponsoredSelection {
    readonly phase: 'idle'|'quoting'|'quoted'|'approved'|'signing'|'submitted'|'recovering'|'confirmed'|'reverted'|'cancelled'|'unknown'|'refused';
    readonly batch?: SponsoredBatch;
    readonly consent?: SponsoredConsent;
    readonly relayerSignature?: string;
    readonly digest?: string;
    readonly reason?: string;
    readonly txHash?: string;
}

function sponsoredReason(cause:unknown):string {
    return cause instanceof GasStationError ? `${cause.refusal.code}:${cause.refusal.field}` : errorMessage(cause);
}
function sameCalls(batch:SponsoredBatch,tx:ModuleTransaction):boolean {
    return batch.calls.length===1&&batch.calls[0]?.to.toLowerCase()===tx.to.toLowerCase()&&batch.calls[0]?.data.toLowerCase()===tx.data.toLowerCase()&&batch.calls[0]?.value===tx.value;
}

export function useFeeSelection(provider: ModuleProvider | null, address: string | null): FeeSelection {
    const [choice, changeChoice] = useState<FeeChoiceId>('pax_gas');
    const connection=useWallet();
    const [sponsored,setSponsored]=useState<SponsoredSelection>({phase:'idle'});
    const sponsoredRef=useRef(sponsored);sponsoredRef.current=sponsored;
    const generation=useRef(0);
    const pendingAbort=useRef<AbortController|null>(null);
    const stationRef=useRef<GasStationModule|null>(null);
    const publish=(value:SponsoredSelection)=>{sponsoredRef.current=value;setSponsored(value);};
    const [feeDenom, setFeeDenom] = useState<string | null>(null);
    const [denomError, setDenomError] = useState<string | null>(null);
    const [updating, setUpdating] = useState(false);
    const surfaceModule = useMemo(() => (provider ? feeToken(provider) : null), [provider]);

    const read = useCallback(async () => {
        if (!surfaceModule || !address) return;
        try {
            const denom = await surfaceModule.getFeeDenom(address);
            setFeeDenom(denom);
            setDenomError(null);
        } catch (cause) {
            setDenomError(errorMessage(cause));
        }
    }, [surfaceModule, address]);

    useEffect(() => {
        setFeeDenom(null);
        void read();
    }, [read]);

    const applyPreference = useCallback(async () => {
        if (!surfaceModule || !provider || !address || choice==='sid_sponsored') return;
        const tx = choice === 'sid_native' ? surfaceModule.setFeeDenom(SIDIORA_FEE_DENOM) : surfaceModule.clearFeeDenom();
        setUpdating(true);
        try {
            const hash = await surfaceModule.send(address, tx);
            const receipt = await waitForReceipt(provider, hash);
            if (receiptStatus(receipt) !== 'confirmed') throw new Error('the fee token preference was not applied');
            await read();
        } catch (cause) {
            setDenomError(errorMessage(cause));
        } finally {
            setUpdating(false);
        }
    }, [surfaceModule, provider, address, choice, read]);

    const station=useCallback(():GasStationModule=>{
        if(!provider||!address||connection.mode!=='embedded')throw new GasStationError({code:'refused',field:'safe_first_delegation'});
        if(stationRef.current)return stationRef.current;
        const gatewayUrl=process.env.NEXT_PUBLIC_PAXEER_WALLET_API;
        const sponsor=process.env.NEXT_PUBLIC_PAXEER_GAS_SPONSOR;
        const paymaster=process.env.NEXT_PUBLIC_PAXEER_GAS_PAYMASTER;
        if(!gatewayUrl||!sponsor||!paymaster)throw new GasStationError({code:'unavailable',field:'station_configuration'});
        stationRef.current=gasStation(provider,{gatewayUrl,chainId:BigInt(PAXEER_CHAIN_ID),sponsor,paymaster});
        return stationRef.current;
    },[provider,address,connection.mode]);
    const cancel=useCallback(()=>{
        pendingAbort.current?.abort();
        const phase=sponsoredRef.current.phase;
        ++generation.current;
        publish({ ...sponsoredRef.current,phase:phase==='signing'||phase==='submitted'||phase==='recovering'||phase==='unknown'?'unknown':'cancelled',reason:phase==='signing'?'submission_status_unknown':'cancelled:consent'});
    },[]);
    useEffect(()=>{
        ++generation.current;pendingAbort.current?.abort();stationRef.current=null;publish({phase:'idle'});
        const invalidate=()=>{++generation.current;pendingAbort.current?.abort();stationRef.current=null;publish({phase:'refused',reason:'refused:session_changed'});};
        connection.wallet?.on('accountsChanged',invalidate);connection.wallet?.on('chainChanged',invalidate);connection.wallet?.on('disconnect',invalidate);
        return()=>{++generation.current;pendingAbort.current?.abort();connection.wallet?.off('accountsChanged',invalidate);connection.wallet?.off('chainChanged',invalidate);connection.wallet?.off('disconnect',invalidate);};
    },[provider,address,connection.identity?.id,connection.wallet]);
    const recover=useCallback(async()=>{
        if(!address)return;
        const ticket=++generation.current;const previous=sponsoredRef.current;let retainedFound=false;publish({...previous,phase:'recovering'});
        try{
            const client=station();const retained=await client.pending(address);
            if(!retained){if(ticket===generation.current)publish({phase:'idle'});return;}
            retainedFound=true;
            const report=await client.resume(retained.batch,retained.relayerSignature);
            if(ticket===generation.current)publish({phase:report.status==='pending'?'submitted':report.status,batch:retained.batch,relayerSignature:retained.relayerSignature,
                digest:client.digest(retained.batch),...(report.tx_hash?{txHash:report.tx_hash}:{})});
        }catch(cause){if(ticket===generation.current)publish({...previous,phase:retainedFound||['signing','submitted','unknown'].includes(previous.phase)?'unknown':'refused',reason:sponsoredReason(cause)});}
    },[address,station]);
    useEffect(()=>{if(choice==='sid_sponsored')void recover();},[choice,recover]);
    const prepare=useCallback(async(tx:ModuleTransaction,maximum:bigint,gasCost:bigint)=>{
        if(!address)return;
        if(['signing','submitted','recovering','unknown'].includes(sponsoredRef.current.phase)){
            if(sponsoredRef.current.batch&&!sameCalls(sponsoredRef.current.batch,tx)){pendingAbort.current?.abort();++generation.current;publish({...sponsoredRef.current,phase:'unknown',reason:'refused:construction_changed'});}
            return;
        }
        pendingAbort.current?.abort();const abort=new AbortController();pendingAbort.current=abort;
        const ticket=++generation.current;publish({phase:'quoting'});
        const call=Object.freeze({to:tx.to,data:tx.data,value:tx.value});
        try{
            const client=station();const outstanding=await client.pending(address);
            if(outstanding){await recover();return;}
            const nonce=await client.batchNonce(address);
            const answer=await client.requestQuote({account:address,nonce,calls:[call],maxTokenAmount:maximum,gasCost},{signal:abort.signal});
            const batch:SponsoredBatch=Object.freeze({chainId:client.config.chainId,account:address.toLowerCase(),nonce,calls:Object.freeze([call]),quote:Object.freeze(answer.quote)});
            if(ticket===generation.current)publish({phase:'quoted',batch,relayerSignature:answer.relayerSignature,digest:client.digest(batch)});
        }catch(cause){if(ticket===generation.current)publish({phase:'refused',reason:sponsoredReason(cause)});}
    },[address,station,recover]);
    const approve=useCallback(()=>{
        const current=sponsoredRef.current;
        if(current.phase!=='quoted'||!current.batch||current.batch.quote.deadline<=BigInt(Math.floor(Date.now()/1000))){publish({...current,phase:'refused',reason:'expired_quote:deadline'});return;}
        publish({...current,phase:'approved'});
    },[]);
    const submit=useCallback(async(tx:ModuleTransaction,direct:()=>Promise<string>):Promise<string>=>{
        if(choice!=='sid_sponsored'){
            const blocked=feeBlocked(choice,feeDenom);if(blocked)throw new Error(blocked);
            return direct();
        }
        const current=sponsoredRef.current;const client=station();
        if(!current.batch||!current.relayerSignature||!sameCalls(current.batch,tx))throw new GasStationError({code:'refused',field:'construction_changed'});
        if(current.phase==='unknown'||current.phase==='submitted'){
            const report=await client.resume(current.batch,current.relayerSignature);
            publish({...current,phase:report.status==='pending'?'submitted':report.status,...(report.tx_hash?{txHash:report.tx_hash}:{})});
            if(!report.tx_hash)throw new GasStationError({code:'unavailable',field:'submission_pending'});return report.tx_hash;
        }
        if(current.phase!=='approved')throw new GasStationError({code:'refused',field:'consent'});
        if(current.batch.quote.deadline<=BigInt(Math.floor(Date.now()/1000))){publish({...current,phase:'refused',reason:'expired_quote:deadline'});throw new GasStationError({code:'expired_quote',field:'deadline'});}
        const digest=client.digest(current.batch);if(current.digest!==digest)throw new GasStationError({code:'refused',field:'construction_changed'});
        const ticket=++generation.current;const abort=new AbortController();pendingAbort.current=abort;publish({...current,phase:'signing'});
        try{
            const hash=await client.submitFirstUse(current.batch,current.relayerSignature,{signal:abort.signal,confirm:consent=>{
                const value=sponsoredRef.current;return ticket===generation.current&&!abort.signal.aborted&&value.phase==='signing'&&consent.batchDigest===digest&&consent.account===address?.toLowerCase()&&sameCalls(current.batch!,tx);
            }});
            if(ticket===generation.current)publish({...current,phase:'submitted',txHash:hash});return hash;
        }catch(cause){let pending=false;try{pending=await client.pending(address!)!==null;}catch{pending=true;}if(ticket===generation.current)publish({...current,phase:pending?'unknown':cause instanceof GasStationError&&cause.refusal.code==='cancelled'?'cancelled':'refused',reason:sponsoredReason(cause)});throw cause;}
    },[choice,feeDenom,station,address]);
    const setChoice=useCallback((next:FeeChoiceId)=>{
        if(next!==choice&&['signing','submitted','recovering','unknown'].includes(sponsoredRef.current.phase)){publish({...sponsoredRef.current,reason:'refused:submission_pending'});return;}
        if(next!==choice)cancel();changeChoice(next);
    },[choice,cancel]);
    const blocked=choice==='sid_sponsored'?(sponsored.phase==='approved'||sponsored.phase==='submitted'||sponsored.phase==='unknown'?null:sponsored.reason??'Approve the exact SID quote before sending.'):feeBlocked(choice,feeDenom);
    return { choice, setChoice, feeDenom, denomError, updating, blocked, applyPreference, sponsored,prepare,approve,cancel,recover,submit };
}


export function useWebDataCaps(): { readonly caps: WalletCapsState; readonly refreshCaps: () => void } {
    const connection = useWallet();
    const { provider, address } = useSurfaceWallet();
    const [revision, setRevision] = useState(0);
    const key = useMemo(() => ({}), [provider, address, connection.identity?.id, connection.status, revision]);
    const [retained, setRetained] = useState<{ key: object; value: WalletCapsState } | null>(null);
    const refreshCaps = useCallback(() => setRevision((value) => value + 1), []);
    useEffect(() => {
        let alive = true, generation = 0, inFlight = false;
        const loading = () => setRetained({ key, value: { state: 'loading' } });
        const read = async () => {
            if (inFlight) return;
            if (!provider || !address) {
                setRetained({ key, value: { state: 'refused', reason: 'Connect an authenticated wallet to read its caps.' } });
                return;
            }
            inFlight = true;
            const request = ++generation;
            loading();
            try {
                const value = await webData(provider).caps(address);
                if (alive && request === generation) setRetained({ key, value });
            } catch (cause) {
                const code = isRecord(cause) && typeof cause.code === 'number' ? cause.code : null;
                const refused = cause instanceof TypeError || code === 4100 || code === 4200 || code === -32002 || code === -32603;
                if (alive && request === generation) setRetained({ key, value: { state: refused ? 'refused' : 'unavailable',
                    reason: refused ? 'The account or its evidence was refused.' : 'Verified caps are currently unavailable.' } });
            } finally { inFlight = false; }
        };
        const invalidate = () => {
            ++generation;
            loading();
            refreshCaps();
        };
        const message = (...args: unknown[]) => {
            if (isRecord(args[0]) && args[0].type === 'wallet_caps_invalidated') invalidate();
        };
        const wallet = connection.wallet;
        wallet?.on('accountsChanged', invalidate);
        wallet?.on('chainChanged', invalidate);
        wallet?.on('disconnect', invalidate);
        wallet?.on('message', message);
        window.addEventListener('focus', invalidate);
        void read();
        const timer = window.setInterval(() => { void read(); }, 5_000);
        return () => {
            alive = false;
            ++generation;
            window.clearInterval(timer);
            window.removeEventListener('focus', invalidate);
            wallet?.off('accountsChanged', invalidate);
            wallet?.off('chainChanged', invalidate);
            wallet?.off('disconnect', invalidate);
            wallet?.off('message', message);
        };
    }, [key, provider, address, connection.wallet, refreshCaps]);
    const caps: WalletCapsState = retained?.key === key ? retained.value : { state: 'loading' };
    useEffect(() => {
        if (caps.state !== 'ready' && caps.state !== 'empty') return undefined;
        const delay = Math.max(0, Math.min(60_000, Number(BigInt(caps.context.expires_at) * 1000n - BigInt(Date.now()))));
        const timer = window.setTimeout(refreshCaps, delay);
        return () => window.clearTimeout(timer);
    }, [caps, refreshCaps]);
    return { caps, refreshCaps };
}
