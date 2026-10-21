'use client';
import Link from 'next/link';
import { ArrowLeft, FileText } from 'lucide-react';
import { Avatar, CountdownProgress, EmptyState, ErrorState, KeyValueRow, LoadMore, Skeleton, StatusBadge, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { shortAddress } from '@/utils/format';
import { bountiesContract, type Bounty } from './bounties-contract';
import { BountyReward } from './bounty-reward';
import { BountyTokenChip } from './bounty-token-chip';
import { useBounty } from './use-bounties';
import { useBountySubmissions, useRewardAsset, type BountySubmissionsResult, type RewardAsset } from './use-bounty';
import { useSessionWallet } from './use-bounty-mutations';
import { SubmissionCard } from './submission-card';
import { SubmissionComposer } from './submission-composer';
import { JudgePanel } from './judge-panel';

function BackLink() {
  return (
    <Link href="/bounties" className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-text-muted hover:text-text focus-visible:outline-2 focus-visible:outline-focus">
      <ArrowLeft aria-hidden="true" className="size-icon-sm" />All bounties
    </Link>
  );
}

function BountyHeader({ bounty }: { bounty: Bounty }) {
  const badge = bountiesContract.badgeFor(bounty);
  const creator = shortAddress(bounty.creatorWallet, 6, 4);
  return (
    <header className="flex flex-col gap-4 rounded-lg bg-surface-1 p-4 md:p-6">
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge status={badge.status}>{badge.label}</StatusBadge>
        {bounty.poolAddress ? <BountyTokenChip poolAddress={bounty.poolAddress} /> : null}
      </div>
      <h1 className="break-words text-2xl font-extrabold text-text">{bounty.title}</h1>
      <div className="flex items-center gap-2 text-sm text-text-muted">
        <Avatar src={null} name={bounty.creatorWallet} size="sm" decorative />
        <span>by</span>
        <Link href={`/profile/${bounty.creatorWallet.toLowerCase()}`} className="font-mono hover:text-text focus-visible:outline-2 focus-visible:outline-focus">{creator}</Link>
      </div>
      <BountyReward bounty={bounty} size="lg" />
      {bounty.deadlineAt ? <CountdownProgress endsAt={bounty.deadlineAt} variant="bar" /> : null}
      <dl className="grid gap-2 text-sm md:grid-cols-2">
        <KeyValueRow label="Reward coin" value={<span className="font-mono" title={bounty.assetAddress}>{shortAddress(bounty.assetAddress, 6, 4)}</span>} />
        {bounty.winnersCount !== undefined ? <KeyValueRow label="Winners" value={bounty.winnersCount} /> : null}
        {bounty.submissionsCount !== undefined ? <KeyValueRow label="Submissions" value={bounty.submissionsCount} /> : null}
      </dl>
    </header>
  );
}

function submissionsEmptyBody(bounty: Bounty | null, creator: boolean): string {
  if (creator) return 'Submissions show up here for you to review';
  if (bounty?.state === 'open') return 'Be the first to submit';
  return 'Nobody submitted to this bounty';
}

function SubmissionsSection({ result, asset, wallet, bounty, creator }: { result: BountySubmissionsResult; asset: RewardAsset; wallet: string | null; bounty: Bounty | null; creator: boolean }) {
  const { loadable } = result;
  const own = (item: { authorWallet: string }) => wallet !== null && item.authorWallet.toLowerCase() === wallet.toLowerCase();
  // Rejected work stays visible only to its author, so the list can be empty while the page is not.
  const visible = loadable.state === 'ready' ? loadable.data.filter(item => item.state !== 'rejected' || own(item)) : [];
  return (
    <section aria-labelledby="bounty-submissions-heading" className="flex flex-col gap-3">
      <h2 id="bounty-submissions-heading" className="text-lg font-bold text-text">Submissions</h2>
      {loadable.state === 'loading' ? (
        <div aria-busy="true" className="flex flex-col gap-3">
          <Skeleton height={120} label="Loading submissions" />
          <Skeleton height={120} />
        </div>
      ) : null}
      {loadable.state === 'unsupported' ? (
        <UnavailableState title="Submissions" operation={loadable.operation} status={loadable.status} message="Check back soon" className="rounded-lg bg-surface-1" />
      ) : null}
      {loadable.state === 'error' ? <ErrorState title="Submissions can't load right now" message="Something went wrong, try again" onRetry={loadable.retry} className="rounded-lg bg-surface-1" /> : null}
      {loadable.state === 'ready' && visible.length === 0 && !result.hasMore ? (
        <EmptyState icon={FileText} title="No submissions yet" body={submissionsEmptyBody(bounty, creator)} className="rounded-lg bg-surface-1" />
      ) : null}
      {loadable.state === 'ready' && (visible.length > 0 || result.hasMore) ? (
        <>
          <ul className="grid gap-3 md:grid-cols-2">
            {visible.map(item => (
              <li key={item.submissionId} className="min-w-0">
                <SubmissionCard submission={item} asset={asset} own={own(item)} />
              </li>
            ))}
          </ul>
          <LoadMore onLoadMore={result.loadMore} hasMore={result.hasMore} loading={result.loadingMore} auto={false} label="Show more" endLabel="No more submissions" />
        </>
      ) : null}
    </section>
  );
}

function DetailSkeleton() {
  return (
    <div aria-busy="true" className="flex flex-col gap-4 rounded-lg bg-surface-1 p-4 md:p-6">
      <Skeleton variant="pill" width={64} height={20} label="Loading bounty" />
      <Skeleton width="70%" height={32} />
      <Skeleton width={160} height={16} />
      <Skeleton width={120} height={28} />
      <Skeleton variant="text" rows={4} />
    </div>
  );
}

/**
 * Bounty detail (/bounties/[bountyId]): IF_BOUNTY_GET for the bounty,
 * IF_BOUNTY_SUBMISSIONS for the list, the composer for everyone else while
 * the bounty is open, and the judge panel for its creator.
 */
export function BountyDetail({ bountyId, className }: { bountyId: string; className?: string }) {
  const detail = useBounty(bountyId);
  const submissions = useBountySubmissions(bountyId);
  const wallet = useSessionWallet();
  const bounty = detail.state === 'ready' ? detail.data.data.bounty : null;
  const asset = useRewardAsset(bounty);
  const creator = bounty !== null && bountiesContract.isCreator(bounty, wallet);

  return (
    <div className={cn('flex flex-col gap-6', className)}>
      <BackLink />
      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,380px)]">
        <div className="flex min-w-0 flex-col gap-6">
          {detail.state === 'loading' ? <DetailSkeleton /> : null}
          {detail.state === 'unsupported' ? (
            <UnavailableState title="This bounty" operation={detail.operation} status={detail.status} message="Check back soon" className="rounded-lg bg-surface-1" />
          ) : null}
          {detail.state === 'error' ? <ErrorState title="This bounty can't load right now" message="Something went wrong, try again" onRetry={detail.retry} className="rounded-lg bg-surface-1" /> : null}
          {bounty ? <BountyHeader bounty={bounty} /> : null}
          {bounty ? (
            <section aria-labelledby="bounty-description-heading" className="flex flex-col gap-2 rounded-lg bg-surface-1 p-4 md:p-6">
              <h2 id="bounty-description-heading" className="text-md font-bold text-text">Tasks and rules</h2>
              <p className="whitespace-pre-wrap break-words text-sm text-text">{bounty.description}</p>
            </section>
          ) : null}
          <SubmissionsSection result={submissions} asset={asset} wallet={wallet} bounty={bounty} creator={creator} />
        </div>
        <aside aria-label="Take part" className="flex min-w-0 flex-col gap-4 lg:sticky lg:top-4 lg:self-start">
          {bounty && creator ? (
            submissions.loadable.state === 'ready' ? (
              <JudgePanel bounty={bounty} revision={detail.state === 'ready' ? detail.data.revision : null} submissions={submissions.loadable.data} asset={asset} />
            ) : (
              <p className="rounded-lg bg-surface-1 p-4 text-sm text-text-faint">Submissions can't load right now, so you can't pick a winner yet</p>
            )
          ) : null}
          {bounty && !creator && bounty.state === 'open' ? <SubmissionComposer bounty={bounty} /> : null}
          {bounty && !creator && bounty.state !== 'open' ? (
            <p className="rounded-lg bg-surface-1 p-4 text-sm text-text-muted">This bounty is {bountiesContract.badgeFor(bounty).label.toLowerCase()} and isn't taking submissions</p>
          ) : null}
        </aside>
      </div>
    </div>
  );
}
