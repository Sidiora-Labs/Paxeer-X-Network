'use client';

import { useState } from 'react';
import {
    statusLadder,
    type IntentEndpoint,
    type IntentEndpointKind,
    type IntentLegBinding,
    type IntentPlan,
    type Journey,
    type UnifiedAccountDocument,
} from '@paxeer/wallet';
import { Button, ErrorState, SegmentedControl, Skeleton, TextField } from '@/components/ui/primitives';
import { useAccountClients } from './AccountProvider';
import { errorMessage, kernelGate, kernelReasonText, useAccountDocument, useKernelState } from './hooks';
import { KernelNotice } from './KernelNotice';
import { StatusLadder } from './StatusLadder';

type DestinationKind = Exclude<IntentEndpointKind, 'paxeer-wallet'>;

const DESTINATION_KINDS: readonly { value: DestinationKind; label: string }[] = [
    { value: 'agent', label: 'Agent' },
    { value: 'human', label: 'Person' },
    { value: 'agent-budget', label: 'Budget' },
];

const ASSET_ID = /^[0-9a-f]{64}$/;
const AMOUNT = /^(0|[1-9][0-9]*)$/;
const CURRENCY = /^[A-Z0-9]{1,16}$/;

type Stage =
    | { readonly step: 'editing' }
    | { readonly step: 'planning' }
    | { readonly step: 'planned'; readonly plan: IntentPlan; readonly deadline: number }
    | { readonly step: 'submitting'; readonly plan: IntentPlan; readonly deadline: number }
    | { readonly step: 'tracking'; readonly plan: IntentPlan; readonly journeyId: string; readonly journey: Journey | null }
    | { readonly step: 'refused'; readonly reason: string; readonly plan: IntentPlan | null };

export interface KernelPlanProps {
    readonly account: string;
    readonly now?: () => number;
    readonly idempotencyKey?: () => string;
}

function endpointLabel(endpoint: IntentEndpoint): string {
    return endpoint.kind === 'paxeer-wallet' ? 'Paxeer wallet' : (endpoint.account ?? 'an unnamed account');
}

function newIdempotencyKey(): string {
    return globalThis.crypto.randomUUID().replace(/-/g, '');
}

export function legBindings(
    plan: IntentPlan,
    document: UnifiedAccountDocument,
    sequence: number,
    window: { notBefore: number; notAfter: number },
    maxFee: { amount: string; currency: string },
): IntentLegBinding[] {
    const actor = document.layerx_did;
    if (!actor || !document.bound) throw new Error('the account is not bound to its LayerX identity');
    let next = sequence;
    return plan.legs.map((leg) => {
        const requirement = plan.signing_requirements.find((entry) => entry.leg_index === leg.index);
        if (!requirement) throw new Error(`leg ${leg.index} has no signer in the plan`);
        if (leg.fee.currency !== maxFee.currency || BigInt(leg.fee.amount) > BigInt(maxFee.amount)) {
            throw new Error(`the fee of leg ${leg.index} exceeds the maximum fee`);
        }
        const binding: IntentLegBinding = {
            leg_index: leg.index,
            action_key: requirement.action_key,
            actor,
            authority: requirement.authority,
            relationship: 'self',
            account_sequence: next,
            not_before: window.notBefore,
            not_after: window.notAfter,
            fee_limit: { amount: maxFee.amount, currency: maxFee.currency },
        };
        if (leg.domain === 'layerx') next += 1;
        return binding;
    });
}

function PlanSummary({ plan }: { plan: IntentPlan }) {
    return (
        <div className="space-y-3" data-plan={plan.plan_digest}>
            <ol aria-label="Plan legs" className="space-y-2">
                {plan.legs.map((leg) => {
                    const signer = plan.signing_requirements.find((entry) => entry.leg_index === leg.index);
                    return (
                        <li key={leg.index} data-leg={leg.index} data-domain={leg.domain} className="rounded-xl bg-white/[0.04] p-3 text-xs">
                            <p className="font-semibold">
                                {leg.index + 1}. {leg.mechanism.replace(/-/g, ' ')} on {leg.domain === 'paxeer' ? 'Paxeer' : 'LayerX'}
                            </p>
                            <p className="text-pax-muted">
                                {endpointLabel(leg.source)} to {endpointLabel(leg.destination)}
                            </p>
                            <p>
                                {leg.money.amount} {leg.money.currency}
                            </p>
                            <p data-fee={leg.index}>
                                Fee {leg.fee.amount} {leg.fee.currency}
                            </p>
                            <p data-signer={leg.index} className="text-pax-muted">
                                Signer {signer ? signer.authority : 'none'}
                            </p>
                        </li>
                    );
                })}
            </ol>
            <p data-total-fee className="text-sm font-semibold">
                Total fee {plan.total_fee.amount} {plan.total_fee.currency}
            </p>
        </div>
    );
}

function JourneyView({ journey }: { journey: Journey }) {
    return (
        <div className="space-y-2" data-journey={journey.journey_id} data-state={journey.state}>
            <StatusLadder steps={[statusLadder.fromJourney(journey.state)]} />
            <ol aria-label="Journey stages" className="space-y-1 text-xs">
                {journey.stages.map((stage) => (
                    <li key={stage.stage_id} data-stage={stage.stage_id} data-state={stage.state} className="text-pax-muted">
                        {stage.copy_key} · {stage.state}
                    </li>
                ))}
            </ol>
        </div>
    );
}

export function KernelPlan({ account, now = Date.now, idempotencyKey = newIdempotencyKey }: KernelPlanProps) {
    const { human, kernelReads } = useAccountClients();
    const kernelState = useKernelState();
    const document = useAccountDocument(account);
    const [kind, setKind] = useState<DestinationKind>('agent');
    const [destination, setDestination] = useState('');
    const [assetId, setAssetId] = useState('');
    const [amount, setAmount] = useState('');
    const [currency, setCurrency] = useState('');
    const [maxFee, setMaxFee] = useState('');
    const [minutes, setMinutes] = useState('15');
    const [stage, setStage] = useState<Stage>({ step: 'editing' });

    const gate = kernelGate(kernelState);
    const minutesValue = Number(minutes);
    const formValid =
        destination.trim().length > 0 &&
        ASSET_ID.test(assetId.trim()) &&
        AMOUNT.test(amount.trim()) &&
        amount.trim() !== '0' &&
        CURRENCY.test(currency.trim()) &&
        AMOUNT.test(maxFee.trim()) &&
        Number.isInteger(minutesValue) &&
        minutesValue >= 1 &&
        minutesValue <= 1440;
    const busy = stage.step === 'planning' || stage.step === 'submitting';

    async function plan() {
        if (!gate.open || !formValid || busy) return;
        const deadline = Math.floor(now() / 1000) + minutesValue * 60;
        setStage({ step: 'planning' });
        try {
            const planned = await human.planIntent({
                source: { kind: 'paxeer-wallet' },
                destination: { kind, account: destination.trim() },
                asset_id: assetId.trim(),
                money: { amount: amount.trim(), currency: currency.trim() },
                constraints: {
                    deadline: new Date(deadline * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z'),
                    max_fee: { amount: maxFee.trim(), currency: currency.trim() },
                    allow_top_up: false,
                },
            });
            setStage({ step: 'planned', plan: planned, deadline });
        } catch (error) {
            setStage({ step: 'refused', reason: errorMessage(error), plan: null });
        }
    }

    async function submit() {
        if (stage.step !== 'planned' || document.status !== 'ready') return;
        const { plan: planned, deadline } = stage;
        setStage({ step: 'submitting', plan: planned, deadline });
        try {
            const mainAccount = document.value.layerx_account;
            if (!mainAccount) throw new Error('the account has no LayerX main account');
            const sequenceRead = await kernelReads.getSequence(mainAccount);
            if (!sequenceRead.available) {
                setStage({ step: 'refused', reason: kernelReasonText(sequenceRead), plan: planned });
                return;
            }
            const sequence = Number(sequenceRead.result.next_sequence);
            if (!Number.isSafeInteger(sequence) || sequence < 0) throw new Error('the kernel answered a malformed account sequence');
            const bindings = legBindings(planned, document.value, sequence, { notBefore: Math.floor(now() / 1000), notAfter: deadline }, {
                amount: maxFee.trim(),
                currency: currency.trim(),
            });
            const submitted = await human.submitPlan(
                { plan_digest: planned.plan_digest, signed_digest: planned.plan_digest, bindings },
                idempotencyKey(),
            );
            if (!submitted.available) {
                setStage({ step: 'refused', reason: kernelReasonText(submitted), plan: planned });
                return;
            }
            const journeyId = submitted.result.journey_id;
            setStage({ step: 'tracking', plan: planned, journeyId, journey: null });
            const journey = await human.getJourney(journeyId);
            setStage({ step: 'tracking', plan: planned, journeyId, journey });
        } catch (error) {
            setStage({ step: 'refused', reason: errorMessage(error), plan: planned });
        }
    }

    async function track(journeyId: string, planned: IntentPlan) {
        try {
            const journey = await human.getJourney(journeyId);
            setStage({ step: 'tracking', plan: planned, journeyId, journey });
        } catch (error) {
            setStage({ step: 'refused', reason: errorMessage(error), plan: planned });
        }
    }

    const shownPlan = stage.step === 'editing' || stage.step === 'planning' ? null : stage.plan;
    const submitGate = stage.step === 'planned' ? gate : null;

    return (
        <section aria-label="Kernel plan" className="space-y-3 rounded-[20px] bg-pax-surface p-4">
            <h2 className="text-sm font-bold">Move value across the network</h2>
            <KernelNotice gate={gate} />
            {document.status === 'loading' && <Skeleton className="h-6 w-full" />}
            {document.status === 'error' && (
                <ErrorState title="The account could not be resolved" message={errorMessage(document.error)} onRetry={document.reload} />
            )}
            <SegmentedControl label="Destination kind" value={kind} options={DESTINATION_KINDS} onChange={setKind} />
            <TextField label="Destination account" name="destination" value={destination} onChange={(event) => setDestination(event.target.value)} />
            <TextField label="Asset id" name="asset" value={assetId} onChange={(event) => setAssetId(event.target.value)} />
            <TextField label="Amount in base units" name="amount" inputMode="numeric" value={amount} onChange={(event) => setAmount(event.target.value)} />
            <TextField label="Currency" name="currency" value={currency} onChange={(event) => setCurrency(event.target.value)} />
            <TextField label="Maximum fee" name="max-fee" inputMode="numeric" value={maxFee} onChange={(event) => setMaxFee(event.target.value)} />
            <TextField label="Valid for minutes" name="minutes" inputMode="numeric" value={minutes} onChange={(event) => setMinutes(event.target.value)} />
            <Button className="w-full" name="plan" disabled={!gate.open || !formValid || busy} onClick={() => void plan()}>
                {stage.step === 'planning' ? 'Planning' : 'Plan'}
            </Button>
            {shownPlan && <PlanSummary plan={shownPlan} />}
            {submitGate && (
                <Button
                    className="w-full"
                    name="submit"
                    disabled={!submitGate.open || document.status !== 'ready'}
                    onClick={() => void submit()}
                >
                    Submit
                </Button>
            )}
            {stage.step === 'submitting' && <Skeleton className="h-10 w-full" />}
            {stage.step === 'refused' && (
                <p role="alert" data-plan-refused className="text-xs text-pax-error">
                    {stage.reason}
                </p>
            )}
            {stage.step === 'tracking' && stage.journey && <JourneyView journey={stage.journey} />}
            {stage.step === 'tracking' && (
                <Button variant="secondary" className="w-full" name="track" onClick={() => void track(stage.journeyId, stage.plan)}>
                    Refresh journey
                </Button>
            )}
        </section>
    );
}
