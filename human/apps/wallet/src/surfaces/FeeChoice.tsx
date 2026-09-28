'use client';

import { useEffect, useMemo, useState } from 'react';
import {
    feeChoice,
    feeTokenAmount,
    type FeeChoice as FeeChoiceEntry,
    type IntentLeg,
    type ModuleProvider,
    type ModuleTransaction,
} from '@paxeer/wallet';
import { Button } from '@/components/ui/primitives';
import { cn } from '@/lib/cn';
import { SurfaceFrame } from './Surface';
import { errorMessage, useFeeSelection, useSurfaceWallet, type FeeSelection } from './useSurface';

export interface FeeChoiceProps {
    readonly provider: ModuleProvider | null;
    readonly address: string | null;
    readonly transaction: ModuleTransaction | null;
    readonly selection: FeeSelection;
    readonly sidRate: string | null;
    readonly legs?: readonly IntentLeg[];
}

interface GasEstimate {
    readonly gasLimit: bigint;
    readonly gasPrice: bigint;
}

const QUANTITY = /^0x(?:0|[1-9a-fA-F][0-9a-fA-F]*)$/u;

function quantity(value: unknown, field: string): bigint {
    if (typeof value !== 'string' || !QUANTITY.test(value)) throw new Error(`${field} answered a malformed quantity`);
    return BigInt(value);
}

export function pathText(entry: FeeChoiceEntry): string {
    switch (entry.path.kind) {
        case 'native_gas':
            return `Signed with ${entry.path.method}; gas is paid in ${entry.denomination.symbol}`;
        case 'sponsored_batch':
            return `Signed with ${entry.path.method} as a ${entry.path.construction} construction and submitted through the ${entry.path.submit}; the sponsor is repaid in ${entry.denomination.symbol}`;
        case 'fee_token_preference':
            return `Signed with ${entry.path.method}; gas is charged in ${entry.path.setFeeDenom} through the fee token precompile ${entry.path.precompile}`;
    }
}

export function denominationText(entry: FeeChoiceEntry): string {
    const { symbol, name, decimals, native, token, denom } = entry.denomination;
    const source = native ? 'native coin' : entry.path.kind === 'sponsored_batch' ? `token ${token ?? ''}` : `fee denom ${denom ?? ''}`;
    return `${symbol} · ${name} · ${decimals} decimals · ${source}`;
}

export function FeeChoice({ provider, address, transaction, selection, sidRate, legs = [] }: FeeChoiceProps) {
    const helper = useMemo(() => feeChoice(), []);
    const [estimate, setEstimate] = useState<GasEstimate | null>(null);
    const [estimateError, setEstimateError] = useState<string | null>(null);

    useEffect(() => {
        let alive = true;
        setEstimate(null);
        setEstimateError(null);
        if (!provider || !address || !transaction) return undefined;
        void (async () => {
            try {
                const gasLimit = quantity(
                    await provider.request({
                        method: 'eth_estimateGas',
                        params: [{ from: address, to: transaction.to, data: transaction.data, value: `0x${transaction.value.toString(16)}` }],
                    }),
                    'eth_estimateGas',
                );
                const gasPrice = quantity(await provider.request({ method: 'eth_gasPrice', params: [] }), 'eth_gasPrice');
                if (alive) setEstimate({ gasLimit, gasPrice });
            } catch (cause) {
                if (alive) setEstimateError(errorMessage(cause));
            }
        })();
        return () => {
            alive = false;
        };
    }, [provider, address, transaction]);

    const gasText = (entry: FeeChoiceEntry): string => {
        if (estimateError) return `gas estimate failed: ${estimateError}`;
        if (!estimate) return transaction ? 'estimating gas' : 'no transaction yet';
        const gasCost = estimate.gasLimit * estimate.gasPrice;
        if (entry.id === 'pax_gas') return helper.denominate('pax_gas', gasCost).display;
        if (sidRate === null) return 'the SID per PAX rate is not configured';
        return helper.denominate(entry.id, feeTokenAmount(gasCost, sidRate)).display;
    };

    const layerxLegs = legs.filter((leg) => leg.domain === 'layerx');
    const { choice, setChoice, feeDenom, denomError, updating, blocked, applyPreference } = selection;
    const preferenceAction =
        choice === 'sid_native' && feeDenom !== null && feeDenom !== helper.choice('sid_native').denomination.denom
            ? 'Use SID for gas'
            : choice === 'pax_gas' && feeDenom !== null && feeDenom !== ''
              ? 'Use PAX for gas'
              : null;

    return (
        <section aria-label="Fee choice" className="space-y-3 rounded-2xl bg-[var(--color-surface-raised)] p-4">
            <h2 className="text-sm font-semibold text-pax-light">Chain gas</h2>
            <ul role="radiogroup" aria-label="Fee path" className="space-y-2">
                {helper.choices.map((entry) => (
                    <li key={entry.id}>
                        <button
                            type="button"
                            role="radio"
                            aria-checked={choice === entry.id}
                            data-fee-choice={entry.id}
                            onClick={() => setChoice(entry.id)}
                            className={cn(
                                'w-full space-y-1 rounded-xl px-3 py-2.5 text-left',
                                choice === entry.id ? 'bg-[var(--color-surface-overlay)]' : 'bg-[var(--color-surface-card)]',
                            )}
                        >
                            <span data-role="label" className="block text-sm font-semibold text-pax-light">
                                {entry.label}
                            </span>
                            <span data-role="denomination" className="block text-xs text-pax-muted">
                                {denominationText(entry)}
                            </span>
                            <span data-role="path" className="block text-xs text-pax-muted">
                                {pathText(entry)}
                            </span>
                            <span data-role="gas" className="block text-xs tabular-nums text-pax-light">
                                {gasText(entry)}
                            </span>
                        </button>
                    </li>
                ))}
            </ul>
            <p data-role="fee-denom" className="text-xs text-pax-muted">
                {feeDenom === null
                    ? denomError
                        ? `fee token unknown: ${denomError}`
                        : 'reading the fee token'
                    : feeDenom === ''
                      ? 'Fee token: none, gas is paid in PAX'
                      : `Fee token: ${feeDenom}`}
            </p>
            {feeDenom !== null && denomError && <p role="alert" className="text-xs text-pax-error">{denomError}</p>}
            {preferenceAction && (
                <Button variant="secondary" data-action="fee-preference" disabled={updating} onClick={() => void applyPreference()}>
                    {preferenceAction}
                </Button>
            )}
            {blocked && (
                <p data-role="fee-blocked" className="text-xs text-pax-muted">
                    {blocked}
                </p>
            )}
            <h2 className="text-sm font-semibold text-pax-light">LayerX fees</h2>
            {layerxLegs.length === 0 ? (
                <p data-role="layerx-none" className="text-xs text-pax-muted">
                    This action has no LayerX leg
                </p>
            ) : (
                <ul aria-label="LayerX fee per leg" className="space-y-1">
                    {layerxLegs.map((leg) => (
                        <li key={leg.index} data-layerx-leg={leg.index} className="flex justify-between text-xs">
                            <span className="text-pax-muted">
                                Leg {leg.index} · {leg.mechanism}
                            </span>
                            <span className="tabular-nums text-pax-light">
                                {leg.fee.amount} {leg.fee.currency}
                            </span>
                        </li>
                    ))}
                </ul>
            )}
        </section>
    );
}

export function FeesView({ sidRate, legs }: { sidRate: string | null; legs?: readonly IntentLeg[] }) {
    const { provider, address } = useSurfaceWallet();
    const selection = useFeeSelection(provider, address);
    return (
        <SurfaceFrame title="Fees" connected={provider !== null}>
            <FeeChoice provider={provider} address={address} transaction={null} selection={selection} sidRate={sidRate} legs={legs} />
        </SurfaceFrame>
    );
}
