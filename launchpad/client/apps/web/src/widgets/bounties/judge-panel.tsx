'use client';
import { useId, useState, type FormEvent } from 'react';
import { Gavel } from 'lucide-react';
import { Button, FormField, NoticeBar, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatTokenAmount, shortAddress } from '@/utils/format';
import { PreparedTransactionReview } from './funding-step';
import { parseDecimalAmount, useAcceptSubmission } from './use-bounty-mutations';
import type { Bounty } from './bounties-contract';
import type { BountySubmission, RewardAsset } from './use-bounty';
import { userFacingMessage } from '@/core/user-facing-error';

export interface JudgePanelProps {
  bounty: Bounty;
  /** Revision of the bounty read (IF_BOUNTY_GET); acceptance is optimistic-concurrency checked against it. */
  revision: string | null;
  submissions: readonly BountySubmission[];
  asset: RewardAsset;
  className?: string;
}

/**
 * Creator-only judging: pick a submission, set its award, accept it through
 * IF_BOUNTY_ACCEPT, then sign the settlement transaction the service
 * prepares. The award can never exceed the escrowed budget.
 */
export function JudgePanel({ bounty, revision, submissions, asset, className }: JudgePanelProps) {
  const id = useId();
  const { mutation } = useAcceptSubmission(bounty);
  const [selected, setSelected] = useState<string | null>(null);
  const [award, setAward] = useState('');
  const [attempted, setAttempted] = useState(false);
  const candidates = submissions.filter(item => item.state === 'submitted');
  const target = candidates.find(item => item.submissionId === selected) ?? null;
  const budget = BigInt(bounty.budgetRaw);
  const awardRaw = asset.decimals !== null ? parseDecimalAmount(award, asset.decimals) : null;
  const awardError = asset.decimals === null
    ? "Can't enter a reward for this coin right now"
    : awardRaw === null ? `Enter an amount with up to ${asset.decimals} decimals`
      : awardRaw > budget ? 'That is more than the bounty reward' : null;
  const judgeable = bounty.state === 'open' || bounty.state === 'reviewing';

  function accept(event: FormEvent) {
    event.preventDefault();
    setAttempted(true);
    if (!target || awardError || awardRaw === null || revision === null || mutation.isPending) return;
    mutation.mutate({ submissionId: target.submissionId, awardRaw: awardRaw.toString(), expectedBountyRevision: revision });
  }

  const budgetLabel = asset.decimals !== null ? `${formatTokenAmount(bounty.budgetRaw, asset.decimals, { compact: false })} ${asset.symbol}` : `${bounty.budgetRaw} units`;

  if (mutation.isSuccess) {
    const { submission, settlementRequest } = mutation.data.data;
    const awarded = asset.decimals !== null ? `${formatTokenAmount(submission.awardRaw ?? '0', asset.decimals, { compact: false })} ${asset.symbol}` : `${submission.awardRaw} units`;
    return (
      <section aria-labelledby={`${id}-heading`} className={cn('flex flex-col gap-4 rounded-lg bg-surface-1 p-4', className)}>
        <h2 id={`${id}-heading`} className="text-md font-bold text-text">Pay the winner</h2>
        <NoticeBar tone="success" announce>{shortAddress(submission.authorWallet, 6, 4)} won {awarded}!</NoticeBar>
        {settlementRequest ? (
          <PreparedTransactionReview
            title="Send the reward"
            request={settlementRequest}
            recipient={{ label: 'Winner', address: submission.authorWallet }}
            amount={<span data-numeric className="tabular-nums">{awarded}</span>}
            actionLabel="Confirm in wallet"
          />
        ) : (
          <NoticeBar tone="warning" variant="card" title="Payout not ready yet">
            The winner is picked, but the payout isn't ready to confirm yet. Check back soon
          </NoticeBar>
        )}
      </section>
    );
  }

  return (
    <section aria-labelledby={`${id}-heading`} className={cn('flex flex-col gap-4 rounded-lg bg-surface-1 p-4', className)}>
      <div className="flex items-center gap-2">
        <Gavel aria-hidden="true" className="size-icon-md text-text-muted" />
        <h2 id={`${id}-heading`} className="text-md font-bold text-text">Pick a winner</h2>
      </div>
      <p className="text-sm text-text-muted">You created this bounty. Reward: <span data-numeric className="font-bold tabular-nums text-text">{budgetLabel}</span></p>
      {!judgeable ? <p className="text-sm text-text-faint">This bounty is {bounty.state}, nothing to review</p> : null}
      {judgeable && revision === null ? (
        <NoticeBar tone="warning">Picking a winner isn't available right now. Refresh and try again</NoticeBar>
      ) : null}
      {judgeable && revision !== null && candidates.length === 0 ? <p className="text-sm text-text-faint">No submissions to review yet</p> : null}
      {judgeable && revision !== null && candidates.length > 0 ? (
        <form noValidate onSubmit={accept} className="flex flex-col gap-4">
          <fieldset className="flex flex-col gap-2">
            <legend className="mb-2 text-sm font-bold text-text">Winner</legend>
            {candidates.map(item => (
              <label key={item.submissionId} className={cn('flex cursor-pointer items-start gap-3 rounded-sm p-3 text-sm transition-colors duration-fast has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-focus', selected === item.submissionId ? 'bg-accent-soft' : 'bg-surface-2 hover:bg-surface-hover')}>
                <input type="radio" name={`${id}-winner`} value={item.submissionId} checked={selected === item.submissionId} onChange={() => setSelected(item.submissionId)} className="mt-1 accent-[var(--sl-accent)]" />
                <span className="flex min-w-0 flex-col">
                  <span className="font-mono text-xs text-text-muted">{shortAddress(item.authorWallet, 6, 4)}</span>
                  <span className="line-clamp-2 text-text">{item.body}</span>
                </span>
              </label>
            ))}
          </fieldset>
          <FormField
            id={`${id}-award`}
            label="Reward"
            mono
            error={attempted ? (target ? awardError : 'Pick a submission first') : null}
            helper={`Up to ${budgetLabel}`}
            trailing={<span className="text-sm font-bold text-text-muted">{asset.symbol}</span>}
            inputProps={{ value: award, onChange: event => setAward(event.target.value.replace(',', '.')), inputMode: 'decimal', placeholder: '0.00', autoComplete: 'off', disabled: asset.decimals === null }}
          />
          {mutation.error && mutation.error.state === 'unsupported' ? (
            <UnavailableState title="Picking winners" operation={mutation.error.operation} status={mutation.error.status} message="Try again in a moment" />
          ) : null}
          {mutation.error && mutation.error.state === 'error' ? <NoticeBar tone="danger" announce title="Couldn't pick winner">{userFacingMessage(mutation.error.error)}</NoticeBar> : null}
          <div className="flex justify-end">
            <Button type="submit" variant="primary" size="md" icon={<Gavel />} loading={mutation.isPending} className="w-full md:w-auto">Pick winner</Button>
          </div>
        </form>
      ) : null}
    </section>
  );
}
