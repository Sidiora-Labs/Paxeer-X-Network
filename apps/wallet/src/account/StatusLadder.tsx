'use client';

import { LADDER_RUNGS, readExplorerStatus, statusLadder, type LadderRung, type LadderSource, type LadderStep } from '@paxeer/wallet';
import { ErrorState, Skeleton } from '@/components/ui/primitives';
import { cn } from '@/lib/cn';
import { useAccountClients } from './AccountProvider';
import { errorMessage, useAsync } from './hooks';

const RUNG_LABELS: Readonly<Record<LadderRung, string>> = { instant: 'Instant', sealed: 'Sealed', final: 'Final' };
const SOURCE_LABELS: Readonly<Record<LadderSource, string>> = {
    receipt: 'receipt',
    explorer: 'explorer',
    journey: 'journey',
    anchor: 'anchor',
};

function rank(rung: LadderRung | null): number {
    return rung === null ? -1 : LADDER_RUNGS.indexOf(rung);
}

export function StatusLadder({ steps }: { steps: readonly LadderStep[] }) {
    return (
        <ol aria-label="Status" className="flex gap-2">
            {LADDER_RUNGS.map((rung) => {
                const reached = steps.filter((step) => rank(step.rung) >= rank(rung));
                const evidence = reached.find((step) => step.rung === rung) ?? statusLadder.highest(reached);
                const source = evidence?.source ?? null;
                return (
                    <li
                        key={rung}
                        data-rung={rung}
                        data-reached={source !== null}
                        data-source={source ?? undefined}
                        data-evidence={evidence?.state}
                        title={evidence?.state}
                        className={cn(
                            'flex-1 rounded-xl px-3 py-2 text-center text-xs',
                            source !== null ? 'bg-pax-accent/20 text-pax-light' : 'bg-white/[0.04] text-pax-muted',
                        )}
                    >
                        <span className="block font-semibold">{RUNG_LABELS[rung]}</span>
                        {source !== null && <span className="block text-[10px] text-pax-muted">{SOURCE_LABELS[source]}</span>}
                    </li>
                );
            })}
        </ol>
    );
}

export function TransactionLadder({ hash }: { hash: string }) {
    const { config, fetch: fetchImpl } = useAccountClients();
    const status = useAsync(
        async () => statusLadder.fromExplorer(await readExplorerStatus(config.explorerUrl, hash, fetchImpl)),
        `explorer:${hash.toLowerCase()}`,
    );
    if (status.status === 'loading') return <Skeleton className="h-10 w-full" />;
    if (status.status === 'error') {
        return <ErrorState title="The transaction status could not be read" message={errorMessage(status.error)} onRetry={status.reload} />;
    }
    return <StatusLadder steps={[status.value]} />;
}
