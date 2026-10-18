'use client';

import { useMemo } from 'react';
import { cn } from '@/lib/utils';
import { plainCopy, technicalDetail } from '@/core/user-facing-error';
import { StatTile, type StatTone, humanError } from '@/ui/primitives';
import { formatUsdl } from '@/utils/format';
import { MaskedAmount } from './balance-visibility';
import { portfolioIssue, sumUnrealized, usePortfolio } from './portfolio-transport';
import { PortfolioIssueState } from './portfolio-issue-state';
import { useProfileIssue } from './profile-issues';

export interface PnlSummaryProps {
  wallet: string;
  isOwner?: boolean;
  className?: string;
}

export function pnlTone(raw: bigint | null): StatTone {
  if (raw === null || raw === 0n) return 'neutral';
  return raw > 0n ? 'positive' : 'negative';
}

/**
 * Realized (portfolio summary) and Unrealized (summed from positions, marked
 * partial when any open position lacks a mark or the book spans more pages),
 * both from the production v3 portfolio snapshot. The snapshot publishes no
 * buy or sell totals, so there is no buy volume figure.
 */
export function PnlSummary({ wallet, isOwner, className }: PnlSummaryProps) {
  const portfolio = usePortfolio(wallet, isOwner);
  const page = portfolio.loadable.state === 'ready' ? portfolio.loadable.data.data : null;
  const realizedRaw = page?.summary.realizedPnlRaw !== undefined && page?.summary.realizedPnlRaw !== null ? BigInt(page.summary.realizedPnlRaw) : null;
  const unrealized = useMemo(() => (page ? sumUnrealized(page.positions, !!page.next) : null), [page]);
  const issue = portfolioIssue(portfolio.loadable, portfolio.owner);

  const portfolioLoading = portfolio.loadable.state === 'loading';
  const portfolioNote = issue ? null : portfolio.loadable.state === 'unsupported'
    ? plainCopy.notAvailable
    : portfolio.loadable.state === 'error' ? humanError(portfolio.loadable.error) : null;
  const collected = useProfileIssue('pnl', !issue && portfolio.loadable.state === 'error' ? { label: 'PnL', error: portfolio.loadable.error, retry: portfolio.loadable.retry } : null);
  const showNote = portfolioNote !== null && !(collected && portfolio.loadable.state === 'error');
  const portfolioDetail = portfolio.loadable.state === 'unsupported' ? technicalDetail(portfolio.loadable.operation, portfolio.loadable.status) : null;

  return (
    <section aria-label="Profit and loss summary" className={cn('rounded-lg bg-surface-1 px-4 py-3', className)}>
      <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3">
        <StatTile
          label="Realized"
          size="row"
          loading={portfolioLoading}
          tone={pnlTone(realizedRaw)}
          value={realizedRaw !== null ? <MaskedAmount>{formatUsdl(realizedRaw, { signed: true })}</MaskedAmount> : null}
        />
        <StatTile
          label={unrealized?.partial ? 'Unrealized (partial)' : 'Unrealized'}
          size="row"
          loading={portfolioLoading}
          tone={pnlTone(unrealized && unrealized.counted > 0 ? unrealized.raw : null)}
          value={unrealized && unrealized.counted > 0 ? <MaskedAmount>{formatUsdl(unrealized.raw, { signed: true })}</MaskedAmount> : null}
        />
      </dl>
      {issue ? <PortfolioIssueState issue={issue} variant="inline" className="mt-2 px-2 pb-1" /> : null}
      {showNote || (page && page.summary.realizedPnlRaw === null) || unrealized?.partial ? (
        <ul className="mt-2 flex flex-col gap-0.5 px-2 pb-1 text-xs text-text-faint">
          {showNote ? <li title={portfolioDetail ?? undefined} data-operation={portfolioDetail ?? undefined}>{portfolio.loadable.state === 'error' ? "Can't load PnL right now." : 'PnL is not available yet.'}</li> : null}
          {page && page.summary.realizedPnlRaw === null ? <li>Realized PnL shows once your full trade history is in.</li> : null}
          {unrealized?.partial ? <li>Unrealized covers {unrealized.counted} of {page?.positions.length ?? 0} positions; some prices are missing.</li> : null}
        </ul>
      ) : null}
    </section>
  );
}
