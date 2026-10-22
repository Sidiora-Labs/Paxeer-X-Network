'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    SIDIORA_FEE_DENOM,
    PAXEER_CHAIN_ID, gasStation, GasStationError,
    type GasStationModule, type SponsoredBatch, type SponsoredConsent,
    webData,
    ethCall,
    ethQuantity,
    LEDGER_PRECOMPILE,
    ledgerLogFloor,
    listHolderRecords,
    type HolderRecords,
    type WalletCapsHistory,
    type WalletCapsState,
    type WalletGrantCap,
    type WebPayerGrant,
    feeToken,
    type FeeChoiceId,
    type ModuleProvider,
    type ModuleTransaction,
} from '@paxeer/wallet';
import { ADDR_PRECOMPILE_ADDRESS } from '@sidiora/layerx-sdk/browser';
import { decodeFunctionResult, encodeFunctionData, getAddress, parseAbi, toEventSelector, type Hex } from 'viem';
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
    readonly approving: boolean;
    readonly sent: SentTransaction | null;
    readonly error: string | null;
    readonly send: (tx: ModuleTransaction) => Promise<void>;
}

export const SPONSORED_APPROVE_UNSUPPORTED = 'the allowance approve cannot be sponsored; choose a PAX or SID gas fee for it';

export function useModuleSend(
    surfaceModule: SurfaceModule | null,
    provider: ModuleProvider | null,
    address: string | null,
    fee?: FeeSelection,
    approval?: (owner: string, tx: ModuleTransaction) => Promise<ModuleTransaction | undefined>,
): ModuleSend {
    const [sending, setSending] = useState(false);
    const [approving, setApproving] = useState(false);
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
                const approve = approval ? await approval(address, tx) : undefined;
                if (approve) {
                    if (fee?.choice === 'sid_sponsored') throw new Error(SPONSORED_APPROVE_UNSUPPORTED);
                    if (alive.current) setApproving(true);
                    const approveHash = fee ? await fee.submit(approve, () => surfaceModule.send(address, approve)) : await surfaceModule.send(address, approve);
                    const approved = await waitForReceipt(provider, approveHash);
                    if (receiptStatus(approved) !== 'confirmed') throw new Error(`the approve ${approveHash} did not confirm`);
                    if (alive.current) setApproving(false);
                }
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
                if (alive.current) {
                    setSending(false);
                    setApproving(false);
                }
            }
        },
        [surfaceModule, provider, address, fee, approval],
    );

    return { sending, approving, sent, error, send };
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
            if(!provider)throw new GasStationError({code:'unavailable',field:'provider'});
            const did=await boundDid(provider,address);
            const nonce=await client.batchNonce(address);
            const answer=await client.requestQuote({account:address,did,nonce,calls:[call],maxTokenAmount:maximum,gasCost},{signal:abort.signal});
            const batch:SponsoredBatch=Object.freeze({chainId:client.config.chainId,account:address.toLowerCase(),nonce,calls:Object.freeze([call]),quote:Object.freeze(answer.quote)});
            if(ticket===generation.current)publish({phase:'quoted',batch,relayerSignature:answer.relayerSignature,digest:client.digest(batch)});
        }catch(cause){if(ticket===generation.current)publish({phase:'refused',reason:sponsoredReason(cause)});}
    },[address,provider,station,recover]);
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


const GRANT_LOOKUP_ABI = parseAbi([
    'function getLayerXDid(address addr) view returns (bytes32 didPublicKey, string did)',
    'function accountId(bytes32 didPublicKey, bytes32 assetId) view returns (bytes32)',
]);
const PAYER_GRANT_ISSUED = toEventSelector('PayerGrantIssued(bytes32)');
const ZERO_WORD: Hex = `0x${'0'.repeat(64)}`;
const REFUSED_CAPS: WalletCapsState = { state: 'refused', reason: 'The account or its evidence was refused.' };
const NO_FINALISED_CHECKPOINT: WalletCapsState = { state: 'unavailable', reason: 'No finalised checkpoint is available yet.' };
const NO_GRANT_EXPIRY = ((1n << 64n) - 1n).toString();
export const GRANT_LOG_WINDOW = 10_000n;
export const GRANT_LOG_BATCH = 8;
// At most 1024 windows (10.24M blocks above the floor, 128 sequential batches); a longer history is reported unavailable, never partial.
export const GRANT_LOG_WINDOW_CEILING = 1024n;
const HASH = /^0x[0-9a-fA-F]{64}$/u;

async function view(provider: ModuleProvider, to: string, data: Hex): Promise<Hex> {
    return (await ethCall(provider, to, data)) as Hex;
}

async function layerXDid(provider: ModuleProvider, address: string): Promise<readonly [Hex, string]> {
    return decodeFunctionResult({
        abi: GRANT_LOOKUP_ABI,
        functionName: 'getLayerXDid',
        data: await view(provider, ADDR_PRECOMPILE_ADDRESS, encodeFunctionData({ abi: GRANT_LOOKUP_ABI, functionName: 'getLayerXDid', args: [getAddress(address)] })),
    });
}

export async function boundDid(provider: ModuleProvider, address: string): Promise<string> {
    const [didPublicKey, did] = await layerXDid(provider, address);
    if (didPublicKey === ZERO_WORD) throw new GasStationError({ code: 'refused', field: 'did' });
    return did;
}

async function issuedGrants(provider: ModuleProvider, fromBlock: bigint, toBlock: bigint): Promise<(readonly [string, string])[]> {
    const logs = await provider.request({
        method: 'eth_getLogs',
        params: [{ address: LEDGER_PRECOMPILE, topics: [PAYER_GRANT_ISSUED], fromBlock: `0x${fromBlock.toString(16)}`, toBlock: `0x${toBlock.toString(16)}` }],
    });
    if (!Array.isArray(logs)) throw new TypeError('invalid grant logs');
    return logs.map((log) => {
        const hash: unknown = isRecord(log) ? log.transactionHash : undefined;
        if (!isLog(log) || typeof hash !== 'string' || !HASH.test(hash)) throw new TypeError('invalid grant log');
        const [topic, grantId, ...rest] = log.topics;
        if (log.address.toLowerCase() !== LEDGER_PRECOMPILE || topic?.toLowerCase() !== PAYER_GRANT_ISSUED || grantId === undefined || rest.length !== 0) {
            throw new TypeError('invalid grant log');
        }
        return [grantId.toLowerCase(), hash.toLowerCase()] as const;
    });
}

// The ledger precompile indexes PayerGrantIssued by grant id only and has no per-account grant view, so a grant is
// attributed to the account by its issuing transaction, which the bound address sends or a sponsor sends to it.
async function issuedBy(provider: ModuleProvider, hash: string, address: string): Promise<boolean> {
    const tx = await provider.request({ method: 'eth_getTransactionByHash', params: [hash] });
    if (!isRecord(tx) || typeof tx.from !== 'string' || (tx.to !== null && typeof tx.to !== 'string')) throw new TypeError('invalid grant transaction');
    return tx.from.toLowerCase() === address || tx.to?.toLowerCase() === address;
}

const holderRecordsServed = new WeakMap<ModuleProvider, boolean>();
const RECORD_ID = /^(?:0x)?([0-9a-fA-F]{64})$/u;

// The account's grant records from px_listHolderRecords when the endpoint serves it. The first read per provider is the
// probe: a -32601 answer marks the method as not served and returns null so the bounded chain scan is used instead.
async function indexedGrants(provider: ModuleProvider, address: string): Promise<HolderRecords | null> {
    if (holderRecordsServed.get(provider) === false) return null;
    try {
        const records = await listHolderRecords(provider, address, 'grant');
        holderRecordsServed.set(provider, true);
        return records;
    } catch (cause) {
        if (!holderRecordsServed.has(provider) && isRecord(cause) && cause.code === -32601) {
            holderRecordsServed.set(provider, false);
            return null;
        }
        throw cause;
    }
}

function grantRecordId(record: Readonly<Record<string, unknown>>): string {
    const match = typeof record.record_id === 'string' ? RECORD_ID.exec(record.record_id) : null;
    if (!match) throw new TypeError('invalid holder record');
    return `0x${match[1]!.toLowerCase()}`;
}

async function inBatches<T, R>(items: readonly T[], read: (item: T) => Promise<R>): Promise<R[]> {
    const out: R[] = [];
    for (let start = 0; start < items.length; start += GRANT_LOG_BATCH) out.push(...(await Promise.all(items.slice(start, start + GRANT_LOG_BATCH).map(read))));
    return out;
}

export async function readGrantCaps(provider: ModuleProvider, address: string): Promise<WalletCapsState> {
    const chainId = await ethQuantity(provider, 'eth_chainId');
    const head = await provider.request({ method: 'eth_getBlockByNumber', params: ['finalized', false] });
    if (head === null) return NO_FINALISED_CHECKPOINT;
    if (!isRecord(head) || typeof head.number !== 'string' || !/^0x[0-9a-fA-F]+$/u.test(head.number)
        || typeof head.stateRoot !== 'string' || !/^0x[0-9a-fA-F]{64}$/u.test(head.stateRoot)) {
        throw new TypeError('invalid block header');
    }
    const [didPublicKey, did] = await layerXDid(provider, address);
    if (didPublicKey === ZERO_WORD) return REFUSED_CAPS;
    const account = decodeFunctionResult({
        abi: GRANT_LOOKUP_ABI,
        functionName: 'accountId',
        data: await view(provider, LEDGER_PRECOMPILE, encodeFunctionData({ abi: GRANT_LOOKUP_ABI, functionName: 'accountId', args: [didPublicKey, ZERO_WORD] })),
    }).toLowerCase();
    const finalised = BigInt(head.number);
    const height = finalised.toString();
    const indexed = await indexedGrants(provider, address);
    let ids: string[];
    let history: WalletCapsHistory;
    if (indexed !== null) {
        ids = [...new Set(indexed.items.map(grantRecordId))];
        history = { source: 'indexer', indexed_height: indexed.indexedHeight?.toString() ?? null, chain_height: indexed.chainHeight.toString() };
    } else {
        const floor = ledgerLogFloor(Number(chainId));
        if (floor === undefined) return { state: 'unavailable', reason: `No ledger log floor is configured for chain ${Number(chainId)}.` };
        const count = finalised < floor ? 0n : (finalised - floor) / GRANT_LOG_WINDOW + 1n;
        if (count > GRANT_LOG_WINDOW_CEILING) return { state: 'unavailable', reason: 'The grant history is longer than this view can scan.' };
        const windows: (readonly [bigint, bigint])[] = [];
        for (let to = finalised; to >= floor; to -= GRANT_LOG_WINDOW) windows.push([to - floor >= GRANT_LOG_WINDOW ? to - GRANT_LOG_WINDOW + 1n : floor, to]);
        const issued = (await inBatches(windows, ([from, to]) => issuedGrants(provider, from, to))).flat();
        const self = address.toLowerCase();
        const hashes = [...new Set(issued.map(([, hash]) => hash))];
        const sent = await inBatches(hashes, (hash) => issuedBy(provider, hash, self));
        const mine = new Set(hashes.filter((_, index) => sent[index]));
        ids = [...new Set(issued.filter(([, hash]) => mine.has(hash)).map(([id]) => id))];
        history = { source: 'chain_scan', floor: floor.toString(), head: height };
    }
    const owned = ids.length === 0 ? [] : (await webData(provider).caps(ids)).grants.filter((grant) => grant.from === account);
    const expires = owned.reduce((earliest, grant) => (grant.expiration < earliest ? grant.expiration : earliest), owned[0]?.expiration ?? 0n);
    return {
        state: owned.length === 0 ? 'empty' : 'ready',
        did,
        account_id: account.slice(2),
        network_id: Number(chainId),
        context: { principal: did, session_id: '', address: address.toLowerCase(), chain_id: Number(chainId), expires_at: owned.length === 0 ? NO_GRANT_EXPIRY : expires.toString() },
        observation: { verification: 'checkpoint_finalised', state_root: head.stateRoot.slice(2).toLowerCase(), sequence: height, observed_head: height, batch: height },
        history,
        budgets: [],
        grants: owned.map((grant): WalletGrantCap => ({
            id: grant.grantId.slice(2),
            owner: grant.from.slice(2),
            recipient: grant.recipient.slice(2),
            asset: grant.asset.slice(2),
            per_draw_maximum: grant.perDrawMaximum.toString(),
            allowance: grant.allowance.toString(),
            drawn_total: grant.drawnTotal.toString(),
            drawn_this_period: grant.drawnThisPeriod.toString(),
            recurring: grant.recurring,
            window_length: grant.windowLength.toString(),
            window_start: grant.windowStart.toString(),
            expiration: grant.expiration.toString(),
            revocation_sequence: grant.revocationSequence.toString(),
            revoked_at_sequence: grant.revokedAt.toString(),
            revoked: grant.revoked,
            invoice_settled: grant.invoiceSettled,
        })),
    };
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
                const value = await readGrantCaps(provider, address);
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
