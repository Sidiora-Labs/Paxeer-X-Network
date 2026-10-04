'use client';

import { useEffect, useMemo, useState } from 'react';
import { formatUnits } from 'viem';
import {
    webData,
    WEB_DATA_KIND_FETCH,
    WEB_DATA_KIND_SEARCH,
    type WalletCapsState,
    type IntentLeg,
    type ModuleTransaction,
} from '@paxeer/wallet';
import { SegmentedControl, TextField } from '@/components/ui/primitives';
import { FeeChoice } from './FeeChoice';
import { BuildError, SendButton, SentResult, SurfaceFrame, TransactionPreview, buildOrError } from './Surface';
import { errorMessage, useFeeSelection, useModuleSend, useSurfaceWallet } from './useSurface';

export type WebDataAction = 'fetch' | 'search' | 'refund';

const ACTIONS: readonly { value: WebDataAction; label: string }[] = [
    { value: 'fetch', label: 'Fetch' },
    { value: 'search', label: 'Search' },
    { value: 'refund', label: 'Refund' },
];

export const DEFAULT_CALLBACK_GAS = '200000';

export interface WebDataViewProps {
    readonly sidRate: string | null;
    readonly caps: WalletCapsState;
    readonly refreshCaps: () => void;
    readonly legs?: readonly IntentLeg[];
}

export function DrawCaps({ caps }: Pick<WebDataViewProps, 'caps'>) {
    return (
        <section aria-label="402 draws" data-caps-state={caps.state} className="space-y-2 rounded-2xl bg-[var(--color-surface-raised)] p-4 text-xs">
            <h2 className="text-sm font-semibold text-pax-light">402 draws</h2>
            {caps.state === 'loading' ? <p role="status">Reading verified account caps</p>
                : caps.state === 'unavailable' || caps.state === 'refused' ? <p role="status">{caps.reason}</p>
                : <>
                    <p data-role="caps-observation">{caps.observation.verification} · sequence {caps.observation.sequence} · batch {caps.observation.batch}</p>
                    <p data-role="caps-account" className="break-all">Account {caps.account_id}</p>
                    {caps.state === 'empty' ? <p data-role="caps-empty">No budgets or grants at this verified observation.</p> : null}
                    {caps.budgets.map((budget) => <div key={budget.id} data-budget={budget.id} className="space-y-1 rounded-xl bg-[var(--color-surface-card)] p-2">
                        <p className="break-all">Asset {budget.asset}</p>
                        <p data-role="budget-cap">Period cap: {budget.limit} units · spent {budget.spent} · remaining {budget.remaining}</p>
                        <p>Window {budget.period_start} + {budget.period_length} · expiry {budget.expiry}</p>
                        <p>{budget.revoked ? 'Revoked' : budget.closed ? 'Closed' : 'Open'}</p>
                    </div>)}
                    {caps.grants.map((grant) => <div key={grant.id} data-grant={grant.id} className="space-y-1 rounded-xl bg-[var(--color-surface-card)] p-2">
                        <p className="break-all">Asset {grant.asset}</p>
                        <p data-role="draw-cap">Per-draw cap: {grant.per_draw_maximum} units</p>
                        <p data-role="allowance-cap">Allowance cap: {grant.allowance} units · drawn {grant.drawn_this_period}</p>
                        <p>{grant.recurring ? `Window ${grant.window_start} + ${grant.window_length}` : 'One allowance'} · expiry {grant.expiration}</p>
                        <p>{grant.revoked ? 'Revoked' : grant.invoice_settled ? 'Settled' : 'Open'}</p>
                    </div>)}
                </>}
        </section>
    );
}

export function WebDataView({ sidRate, caps, refreshCaps, legs }: WebDataViewProps) {
    const { provider, address } = useSurfaceWallet();
    const surfaceModule = useMemo(() => (provider ? webData(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const state = useModuleSend(surfaceModule, provider, address, fee);
    useEffect(() => {
        if (state.sent?.status === 'confirmed') refreshCaps();
    }, [state.sent?.hash, state.sent?.status, refreshCaps]);
    const [action, setAction] = useState<WebDataAction>('fetch');
    const [query, setQuery] = useState('');
    const [callbackGas, setCallbackGas] = useState(DEFAULT_CALLBACK_GAS);
    const [requestId, setRequestId] = useState('');
    const [callFee, setCallFee] = useState<bigint | null>(null);
    const [feeError, setFeeError] = useState<string | null>(null);

    useEffect(() => {
        let alive = true;
        setCallFee(null);
        setFeeError(null);
        if (!surfaceModule) return undefined;
        surfaceModule.fee().then(
            (value) => {
                if (alive) setCallFee(value);
            },
            (cause: unknown) => {
                if (alive) setFeeError(errorMessage(cause));
            },
        );
        return () => {
            alive = false;
        };
    }, [surfaceModule]);

    const built = useMemo(() => {
        if (!surfaceModule) return { value: null, error: null };
        return buildOrError<ModuleTransaction | null>(() => {
            if (action === 'refund') return requestId ? surfaceModule.refund(BigInt(requestId)) : null;
            if (!query || callFee === null) return null;
            const kind = action === 'fetch' ? WEB_DATA_KIND_FETCH : WEB_DATA_KIND_SEARCH;
            return surfaceModule.request(kind, query, BigInt(callbackGas), callFee);
        });
    }, [surfaceModule, action, query, callbackGas, requestId, callFee]);

    return (
        <SurfaceFrame title="Web data" connected={surfaceModule !== null}>
            <SegmentedControl label="Web data action" value={action} options={ACTIONS} onChange={setAction} />
            {action === 'refund' ? (
                <TextField label="Request id" name="requestId" value={requestId} onChange={(e) => setRequestId(e.target.value)} />
            ) : (
                <>
                    <TextField
                        label={action === 'fetch' ? 'URL' : 'Search query'}
                        name="query"
                        value={query}
                        onChange={(e) => setQuery(e.target.value)}
                    />
                    <TextField label="Callback gas" name="callbackGas" value={callbackGas} onChange={(e) => setCallbackGas(e.target.value)} />
                    <p data-role="call-value" className="text-sm text-pax-light">
                        {feeError
                            ? `The request fee could not be read: ${feeError}`
                            : callFee === null
                              ? 'Reading the request fee'
                              : `This call carries ${formatUnits(callFee, 18)} PAX`}
                    </p>
                </>
            )}
            <BuildError error={built.error} />
            <TransactionPreview transaction={built.value} />
            <FeeChoice provider={provider} address={address} transaction={built.value} selection={fee} sidRate={sidRate} legs={legs} />
            <SendButton label={action === 'refund' ? 'Request refund' : 'Send request'} transaction={built.value} blocked={fee.blocked} state={state} />
            <SentResult state={state} />
            <DrawCaps caps={caps} />
            <button type="button" onClick={refreshCaps}>Refresh caps</button>
        </SurfaceFrame>
    );
}
