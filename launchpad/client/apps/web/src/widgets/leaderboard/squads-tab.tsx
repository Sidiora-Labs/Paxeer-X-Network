'use client';
import { cn } from '@/lib/utils';
import { UnavailableState } from '@/ui/primitives';

export interface SquadsTabProps { className?: string }

/**
 * Squad rankings have no contract operation: the SDK carries squad CRUD only
 * (IF_SQUAD_CREATE, IF_SQUAD_READ, IF_SQUAD_MEMBERSHIP) and no service ranks
 * summed member PnL. There is no path to request, so the panel states that
 * directly instead of inventing one.
 */
export function SquadsTab({ className }: SquadsTabProps) {
  return (
    <section aria-label="Squads" className={cn('rounded-lg bg-surface-1', className)}>
      <UnavailableState
        title="Squad rankings"
        plural="auto"
        operation="squad ranking read model (no contract operation)"
        status={null}
        message="Check back soon"
      />
    </section>
  );
}
