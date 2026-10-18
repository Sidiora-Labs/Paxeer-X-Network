'use client';
import { useCallback, useState, type ReactNode } from 'react';
import { Plus } from 'lucide-react';
import { Button, NoticeBar } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import type { Loadable } from '@/core/market/unsupported';
import { technicalDetail } from '@/core/user-facing-error';
import { openAuthModal } from '@/shell/auth-modal-store';
import { bountyTabCopy, type BountyTab } from './bounties-tabs';

type Availability = Loadable<{ generatedAt: string }>;

export const createBountyHref = '/bounties/new';
/** Same reason the create form passes to the sign-in modal. */
export const createBountySignInReason = 'Sign in with your wallet to create a bounty';

export interface CreateGateNotice { message: string; detail: string | null }

/**
 * Honest copy for a create attempt the service cannot take right now. Null
 * when the probe answered and creating can go ahead.
 */
export function createBlockedNotice(availability: Availability): CreateGateNotice | null {
  switch (availability.state) {
    case 'ready': return null;
    case 'loading': return { message: 'Still checking, try again in a moment', detail: null };
    case 'unsupported': return { message: "Creating bounties isn't available yet", detail: technicalDetail(availability.operation, availability.status) };
    case 'error': return { message: "Can't create a bounty right now, try again", detail: null };
  }
}

export interface CreateBountyGate {
  /** The service answered and a session is signed in: the CTA navigates to the create page. */
  ready: boolean;
  /** Set after a blocked click; the CTA itself is never disabled. */
  notice: CreateGateNotice | null;
  /**
   * Click handler when the CTA cannot navigate: opens the shared sign-in modal
   * when only the session is missing, otherwise shows the notice for the
   * current service state.
   */
  attempt(): void;
  dismiss(): void;
}

/**
 * Gates the create action at click time instead of disabling the call to
 * action. Posting needs a signed-in wallet session, the same check the create
 * form makes, so a signed-out click opens the existing sign-in modal.
 */
export function useCreateBountyGate(availability: Availability, signedIn: boolean): CreateBountyGate {
  const [attempted, setAttempted] = useState(false);
  const blocked = createBlockedNotice(availability);
  const needsSignIn = blocked === null && !signedIn;
  const attempt = useCallback(() => {
    if (needsSignIn) { openAuthModal(createBountySignInReason); return; }
    setAttempted(true);
  }, [needsSignIn]);
  const dismiss = useCallback(() => setAttempted(false), []);
  return { ready: blocked === null && signedIn, notice: attempted ? blocked : null, attempt, dismiss };
}

/** The create call to action: a link when creating can go ahead, otherwise an enabled button that gates at click time. */
export function CreateBountyButton({ gate, label, pill = false, className }: { gate: CreateBountyGate; label: string; pill?: boolean; className?: string }) {
  if (gate.ready) return <Button variant="primary" size="md" pill={pill} icon={<Plus />} href={createBountyHref} className={className}>{label}</Button>;
  return <Button variant="primary" size="md" pill={pill} icon={<Plus />} onClick={gate.attempt} className={className}>{label}</Button>;
}

/**
 * Notice shown after a blocked create attempt. On phones it floats above the
 * "Post bounty" button so it reads where the tap happened; from md it sits in
 * the header row flow.
 */
export function CreateBountyNotice({ gate, className }: { gate: CreateBountyGate; className?: string }) {
  if (!gate.notice) return null;
  return (
    <NoticeBar
      tone="warning"
      wrap
      announce
      onDismiss={gate.dismiss}
      className={cn(
        'fixed inset-x-4 bottom-[calc(8.5rem+env(safe-area-inset-bottom,0px))] z-[var(--sl-z-toast)] md:static md:inset-auto md:z-auto',
        className,
      )}
    >
      <span title={gate.notice.detail ?? undefined} data-operation={gate.notice.detail ?? undefined}>{gate.notice.message}</span>
    </NoticeBar>
  );
}

/** Phone-only floating "Post bounty" pill, bottom-right above the bottom navigation. */
export function CreateBountyFab({ gate }: { gate: CreateBountyGate }) {
  return (
    <div className="fixed bottom-[calc(4.5rem+env(safe-area-inset-bottom,0px))] right-4 z-[var(--sl-z-sticky)] md:hidden" data-testid="post-bounty-fab">
      <CreateBountyButton gate={gate} label="Post bounty" pill className="h-12 px-5" />
    </div>
  );
}

export interface BountiesHeaderProps {
  tab: BountyTab;
  gate: CreateBountyGate;
  /** Compact stat chips, shown on the header row from lg. */
  stats?: ReactNode;
  /** Inline text totals, shown under the subtitle below lg. */
  inlineStats?: ReactNode;
  className?: string;
}

/**
 * One row from lg: title block on the left, compact stat chips and the
 * always-on Create button on the right. Below lg the totals collapse to two
 * lines of text under the subtitle, and on phones the call to action moves to
 * the floating "Post bounty" pill.
 */
export function BountiesHeader({ tab, gate, stats, inlineStats, className }: BountiesHeaderProps) {
  const copy = bountyTabCopy[tab];
  return (
    <header className={cn('flex flex-col gap-3', className)}>
      <div className="flex items-end justify-between gap-4">
        <div className="flex min-w-0 flex-col gap-1">
          <p className="text-2xs font-bold uppercase tracking-[.08em] text-text-faint">Bounties</p>
          <h1 className="text-xl font-bold text-text md:text-[28px] md:leading-9">{copy.title}</h1>
          <p className="text-sm text-text-muted">{copy.subtitle}</p>
          {inlineStats ? <div className="pt-1 lg:hidden">{inlineStats}</div> : null}
        </div>
        <div className="hidden shrink-0 items-center gap-2 md:flex">
          {stats ? <div className="hidden lg:block">{stats}</div> : null}
          <CreateBountyButton gate={gate} label="Create bounty" />
        </div>
      </div>
      <CreateBountyNotice gate={gate} />
    </header>
  );
}
