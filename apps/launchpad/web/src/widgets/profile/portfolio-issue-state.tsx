'use client';

import { CalendarClock, Hourglass } from 'lucide-react';
import { EmptyState } from '@/ui/primitives';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import type { PortfolioIssue } from './portfolio-transport';

export interface PortfolioIssueStateProps {
  issue: PortfolioIssue;
  variant?: 'panel' | 'inline';
  className?: string;
}

/**
 * The honest state for a portfolio read the production pnl-tracker declined:
 * a sign-in (or sign-in-again) prompt for the owner, or a neutral notice that
 * no current snapshot exists for the wallet. Never an error, never data.
 */
export function PortfolioIssueState({ issue, variant = 'panel', className }: PortfolioIssueStateProps) {
  if (issue.kind === 'signed-out' || issue.kind === 'reauth') {
    return <SignedOutState title={issue.title} body={issue.body} variant={variant} className={className} />;
  }
  return (
    <EmptyState
      icon={issue.kind === 'stale-snapshot' ? CalendarClock : Hourglass}
      title={variant === 'inline' ? `${issue.title}.` : issue.title}
      body={issue.body}
      variant={variant}
      className={className}
    />
  );
}
