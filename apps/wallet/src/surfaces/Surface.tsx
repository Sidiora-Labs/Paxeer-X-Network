'use client';

import type { ReactNode } from 'react';
import type { ModuleTransaction } from '@paxeer/wallet';
import { ApprovalSummary, Button } from '@/components/ui/primitives';
import { formatField, type ModuleSend } from './useSurface';

export function SurfaceFrame({ title, connected, children }: { title: string; connected: boolean; children: ReactNode }) {
    return (
        <section aria-label={title} className="space-y-4 px-4 pb-24 pt-4">
            <h1 className="text-lg font-bold text-pax-light">{title}</h1>
            {connected ? children : <p data-role="not-connected" className="text-sm text-pax-muted">Connect a wallet to use {title}</p>}
        </section>
    );
}

export function TransactionPreview({ transaction, symbol = 'PAX' }: { transaction: ModuleTransaction | null; symbol?: string }) {
    if (!transaction) return null;
    return (
        <div data-role="preview">
            <ApprovalSummary
                rows={[
                    { label: 'To', value: transaction.to },
                    { label: 'Method', value: transaction.data.slice(0, 10) },
                    { label: 'Value', value: `${transaction.value.toString(10)} wei ${symbol}`, emphasis: true },
                ]}
            />
        </div>
    );
}

export function SendButton({
    label,
    transaction,
    blocked,
    state,
}: {
    label: string;
    transaction: ModuleTransaction | null;
    blocked: string | null;
    state: ModuleSend;
}) {
    return (
        <Button
            data-action="send"
            className="w-full"
            disabled={!transaction || blocked !== null || state.sending}
            onClick={() => {
                if (transaction) void state.send(transaction);
            }}
        >
            {state.sending ? 'Sending' : label}
        </Button>
    );
}

export function SentResult({ state }: { state: ModuleSend }) {
    return (
        <>
            {state.error && (
                <p role="alert" data-role="send-error" className="text-sm text-pax-error">
                    {state.error}
                </p>
            )}
            {state.sent && (
                <div data-role="sent" className="space-y-2 rounded-2xl bg-[var(--color-surface-raised)] p-4 text-xs">
                    <p className="break-all text-pax-light">
                        <span data-role="hash">{state.sent.hash}</span> · <span data-role="status">{state.sent.status}</span>
                    </p>
                    <ul aria-label="Events" className="space-y-2">
                        {state.sent.events.map((event, index) => (
                            <li key={`${event.event}-${index}`} data-event={event.event} className="rounded-xl bg-[var(--color-surface-card)] p-2">
                                <p className="font-semibold text-pax-light">{event.event}</p>
                                <dl>
                                    {Object.entries(event.fields).map(([name, value]) => (
                                        <div key={name} className="flex justify-between gap-2">
                                            <dt className="text-pax-muted">{name}</dt>
                                            <dd data-field={name} className="break-all text-right text-pax-light">
                                                {formatField(value)}
                                            </dd>
                                        </div>
                                    ))}
                                </dl>
                            </li>
                        ))}
                    </ul>
                </div>
            )}
        </>
    );
}

export function buildOrError<T>(build: () => T): { value: T | null; error: string | null } {
    try {
        return { value: build(), error: null };
    } catch (cause) {
        return { value: null, error: cause instanceof Error && cause.message ? cause.message : 'the input is invalid' };
    }
}

export function BuildError({ error }: { error: string | null }) {
    if (!error) return null;
    return (
        <p data-role="build-error" className="text-xs text-pax-error">
            {error}
        </p>
    );
}
