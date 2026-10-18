'use client';
import { Lock } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useAuthModal } from '@/shell/auth-modal-store';
import { Button, EmptyState } from '@/ui/primitives';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import RewardsView from '@/widgets/rewards/rewards-view';

export interface RewardsTabProps { className?: string }

/** Rewards tab: the account session gates the entitlement view; signed-out viewers get the sign-in prompt. */
export function RewardsTab({ className }: RewardsTabProps) {
  const { session } = useAccountSession();
  const { openModal } = useAuthModal();
  if (!session) {
    return (
      <section aria-label="Rewards" className={cn('rounded-lg bg-surface-1', className)}>
        <EmptyState
          icon={Lock}
          title="Sign in to see your rewards"
          body="Your cup prizes and creator rewards show up here"
          action={<Button variant="primary" size="md" onClick={() => openModal('rewards')}>Sign in</Button>}
        />
      </section>
    );
  }
  return (
    <section aria-label="Rewards" className={cn('rounded-lg bg-surface-1 p-3 sm:p-4', className)}>
      <RewardsView headingLevel={2} />
    </section>
  );
}
