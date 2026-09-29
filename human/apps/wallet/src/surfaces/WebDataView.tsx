'use client';

import { useEffect, useMemo, useState } from 'react';
import { formatUnits } from 'viem';
import type { PayerGrant } from '@sidiora/layerx-sdk';
import {
    webData,
    WEB_DATA_KIND_FETCH,
    WEB_DATA_KIND_SEARCH,
    type HumanMoney,
    type IntentLeg,
    type KernelAvailabilityState,
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
    readonly kernel: KernelAvailabilityState | null;
    readonly grants: readonly PayerGrant[];
    readonly budget: HumanMoney | null;
    readonly legs?: readonly IntentLeg[];
}

export function DrawCaps({ kernel, grants, budget }: Pick<WebDataViewProps, 'kernel' | 'grants' | 'budget'>) {
    return (
        <section aria-label="402 draws" className="space-y-2 rounded-2xl bg-[var(--color-surface-raised)] p-4 text-xs">
            <h2 className="text-sm font-semibold text-pax-light">402 draws</h2>
            {kernel === null ? (
                <p data-role="kernel-state" className="text-pax-muted">Reading the kernel state</p>
            ) : !kernel.available ? (
                <p data-role="kernel-state" className="text-pax-muted">
                    402 draws are unavailable: {kernel.reason}
                    {kernel.backend ? ` (${kernel.backend})` : ''}
                </p>
            ) : (
                <>
                    <p data-role="budget" className="text-pax-light">
                        {budget ? `Kernel budget: ${budget.amount} ${budget.currency}` : 'Kernel budget: none set'}
                    </p>
                    {grants.length === 0 ? (
                        <p data-role="no-grants" className="text-pax-muted">No 402 grant is open</p>
                    ) : (
                        <ul className="space-y-2">
                            {grants.map((grant) => (
                                <li key={grant.grant_id} data-grant={grant.grant_id} className="space-y-1 rounded-xl bg-[var(--color-surface-card)] p-2">
                                    <p className="break-all text-pax-muted">Asset {grant.asset}</p>
                                    <p data-role="draw-cap" className="text-pax-light">Per-draw cap: {grant.per_draw_maximum}</p>
                                    <p data-role="allowance-cap" className="text-pax-light">Allowance cap: {grant.allowance}</p>
                                    <p className="text-pax-muted">
                                        {grant.recurring ? `Renews every ${grant.window_length} s` : 'One allowance'} · expires at {grant.expiration}
                                    </p>
                                </li>
                            ))}
                        </ul>
                    )}
                </>
            )}
        </section>
    );
}

export function WebDataView({ sidRate, kernel, grants, budget, legs }: WebDataViewProps) {
    const { provider, address } = useSurfaceWallet();
    const surfaceModule = useMemo(() => (provider ? webData(provider) : null), [provider]);
    const fee = useFeeSelection(provider, address);
    const state = useModuleSend(surfaceModule, provider, address);
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
            <DrawCaps kernel={kernel} grants={grants} budget={budget} />
        </SurfaceFrame>
    );
}
