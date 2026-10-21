'use client';
import { useId, useState, type ReactNode } from 'react';
import Link from 'next/link';
import { ImageOff, Trophy } from 'lucide-react';
import { Avatar, Chip, StatusBadge, type BadgeStatus } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatTokenAmount, shortAddress } from '@/utils/format';
import type { BountySubmission, RewardAsset } from './use-bounty';

const SAFE_URL = /^https:\/\//i;

function badgeOf(state: BountySubmission['state']): { status: BadgeStatus; label: string } {
  switch (state) {
    case 'accepted': return { status: 'paid', label: 'Winner' };
    case 'rejected': return { status: 'neutral', label: 'Rejected' };
    case 'withdrawn': return { status: 'neutral', label: 'Withdrawn' };
    case 'submitted': return { status: 'submission', label: 'Submission' };
  }
}

function Evidence({ urls }: { urls: readonly string[] }) {
  const [failed, setFailed] = useState(false);
  const safe = urls.filter(url => SAFE_URL.test(url));
  if (safe.length === 0) return null;
  const extra = safe.length - 1;
  return (
    <div className="relative overflow-hidden rounded-md bg-surface-2">
      {failed ? (
        <div className="flex aspect-[4/3] items-center justify-center text-text-faint"><ImageOff aria-hidden="true" className="size-icon-lg" /><span className="sr-only">Image didn't load</span></div>
      ) : (
        <a href={safe[0]} target="_blank" rel="noopener noreferrer" className="block focus-visible:outline-2 focus-visible:outline-focus">
          <img src={safe[0]} alt="Submission image" loading="lazy" className="max-h-80 w-full object-cover" onError={() => setFailed(true)} />
        </a>
      )}
      {extra > 0 ? (
        <span className="absolute right-2 top-2">
          <Chip variant="token">+{extra}<span className="sr-only"> more attachments</span></Chip>
        </span>
      ) : null}
      {extra > 0 ? (
        <ul aria-label="More attachments" className="flex flex-wrap gap-2 p-2">
          {safe.slice(1).map((url, index) => (
            <li key={url}>
              <a href={url} target="_blank" rel="noopener noreferrer" className="block size-14 overflow-hidden rounded-sm bg-surface-3 focus-visible:outline-2 focus-visible:outline-focus">
                <img src={url} alt={`Attachment ${index + 2}`} loading="lazy" className="size-full object-cover" />
              </a>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

export interface SubmissionCardProps {
  submission: BountySubmission;
  /** Escrowed asset of the parent bounty, for the award amount. */
  asset: RewardAsset;
  /** Viewer's own submission (rejected ones stay visible to their author only in muted text). */
  own?: boolean;
  /** Creator controls (judge panel) rendered under the body. */
  actions?: ReactNode;
  className?: string;
}

/** Submission card: author, state, quoted proof text with an accent bar, evidence images and the award once accepted. */
export function SubmissionCard({ submission, asset, own = false, actions, className }: SubmissionCardProps) {
  const titleId = useId();
  const badge = badgeOf(submission.state);
  const author = shortAddress(submission.authorWallet, 6, 4);
  const muted = submission.state === 'rejected' || submission.state === 'withdrawn';
  const award = submission.awardRaw !== null && asset.decimals !== null
    ? `${formatTokenAmount(submission.awardRaw, asset.decimals, { compact: false })} ${asset.symbol}`
    : submission.awardRaw !== null ? `${submission.awardRaw} ${asset.symbol} units` : null;

  return (
    <article aria-labelledby={titleId} data-state={submission.state} className={cn('flex min-w-0 flex-col gap-3 rounded-lg bg-surface-1 p-3', className)}>
      <header className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <Avatar src={null} name={submission.authorWallet} size="sm" decorative />
          <h3 id={titleId} className="truncate text-sm font-bold text-text">
            <Link href={`/profile/${submission.authorWallet.toLowerCase()}`} className="hover:underline focus-visible:outline-2 focus-visible:outline-focus">{author}</Link>
            {own ? <span className="ml-1 font-normal text-text-faint">(you)</span> : null}
          </h3>
        </div>
        <StatusBadge status={badge.status}>
          {submission.state === 'accepted' ? <Trophy aria-hidden="true" className="size-3" /> : null}
          {badge.label}
        </StatusBadge>
      </header>
      <Evidence urls={submission.evidenceUrls} />
      <blockquote className={cn('flex gap-3 rounded-sm bg-surface-2 p-3 text-sm', muted ? 'text-text-faint' : 'text-text')}>
        <span aria-hidden="true" className="w-[3px] shrink-0 rounded-pill bg-accent" />
        <p className="min-w-0 whitespace-pre-wrap break-words">{submission.body}</p>
      </blockquote>
      {award ? (
        <p className="text-sm text-text-muted">Awarded <span data-numeric className="font-bold tabular-nums text-success">{award}</span></p>
      ) : null}
      {actions}
    </article>
  );
}
