'use client';

import { ErrorState, Skeleton } from '@/components/ui/primitives';
import { errorMessage, kernelGate, useAccountDocument, useKernelState } from './hooks';
import { KernelNotice } from './KernelNotice';

function Field({ label, value, name }: { label: string; value: string; name: string }) {
    return (
        <div className="space-y-1">
            <dt className="text-[11px] font-semibold uppercase tracking-[0.06em] text-pax-muted">{label}</dt>
            <dd data-field={name} className="break-all font-mono text-xs text-pax-light">
                {value}
            </dd>
        </div>
    );
}

export function AccountView({ account }: { account: string }) {
    const document = useAccountDocument(account);
    const kernel = useKernelState();

    return (
        <section aria-label="Unified account" className="space-y-3 rounded-[20px] bg-pax-surface p-4">
            <h2 className="text-sm font-bold">Unified account</h2>
            {document.status === 'loading' && <Skeleton className="h-24 w-full" />}
            {document.status === 'error' && (
                <ErrorState title="The account could not be resolved" message={errorMessage(document.error)} onRetry={document.reload} />
            )}
            {document.status === 'ready' && (
                <dl className="space-y-3">
                    <Field label="Address" name="address" value={document.value.evm_address ?? account} />
                    <Field label="Paxeer address" name="pax-address" value={document.value.pax_address ?? 'Not derived'} />
                    <Field label="LayerX identity" name="did" value={document.value.layerx_did ?? 'No LayerX identity yet'} />
                    <Field label="Main account" name="main-account" value={document.value.layerx_account ?? 'No main account yet'} />
                    <Field
                        label="Binding"
                        name="binding"
                        value={document.value.bound ? 'Bound to its LayerX identity' : 'Not bound to a LayerX identity'}
                    />
                </dl>
            )}
            <KernelNotice gate={kernelGate(kernel)} />
        </section>
    );
}
