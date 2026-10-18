'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { cn } from '@/lib/utils';
import { Avatar, EmptyState, ErrorState, SectionHeader, Skeleton, UnavailableState } from '@/ui/primitives';
import { formatUsdl, shortAddress } from '@/utils/format';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { MaskedAmount } from './balance-visibility';
import { coinHref, tokenIdentity } from './positions-open-closed';
import { portfolioIssue, topTrades, usePortfolio, type TopTrade } from './portfolio-transport';
import { PortfolioIssueState } from './portfolio-issue-state';
import { useProfileIssue } from './profile-issues';

export interface TopTradesProps {
  wallet: string;
  isOwner?: boolean;
  limit?: number;
  className?: string;
}

/** Static (not shimmering) row frames shown while the list could not load, so the rail keeps its shape. */
function PlaceholderRows({ rows }: { rows: number }) {
  return (
    <ol aria-hidden="true" data-top-trades-placeholder className="flex flex-col gap-0.5">
      {Array.from({ length: rows }).map((_, index) => (
        <li key={index} className="flex min-h-14 items-center gap-3 px-3 py-2">
          <span className="w-4 shrink-0 text-center text-xs text-text-faint">{index + 1}</span>
          <span className="size-avatar-lg shrink-0 rounded-sm bg-surface-2" />
          <span className="flex min-w-0 flex-1 flex-col gap-1.5">
            <span className="h-3 w-20 rounded-xs bg-surface-2" />
            <span className="h-2.5 w-12 rounded-xs bg-surface-2" />
          </span>
          <span className="shrink-0 text-sm text-text-faint">—</span>
        </li>
      ))}
    </ol>
  );
}

function TradeRow({ trade, name, symbol, logo, poolAddress }: { trade: TopTrade; name: string; symbol: string | null; logo: string | null; poolAddress: string | null }) {
  const tone = trade.realizedRaw > 0n ? 'text-success' : trade.realizedRaw < 0n ? 'text-danger' : 'text-text';
  return (
    <li className="flex min-h-14 items-center gap-3 rounded-md px-3 py-2 hover:bg-surface-2">
      <Link href={coinHref(trade.position.tokenAddress, poolAddress)} className="flex min-w-0 flex-1 items-center gap-3 rounded-sm">
        <Avatar src={logo} name={symbol ?? name} size="lg" shape="square" decorative />
        <span className="min-w-0">
          <span className="block truncate text-sm font-bold text-text">{name}{symbol ? <span className="ml-1 font-semibold text-text-muted">{symbol}</span> : null}</span>
          <span className="block truncate font-mono-sl text-xs text-text-faint">{shortAddress(trade.position.tokenAddress, 6, 4)}</span>
        </span>
      </Link>
      <span className="flex shrink-0 flex-col items-end">
        <MaskedAmount className={cn('text-num-md font-bold tabular-nums', tone)}>{formatUsdl(trade.realizedRaw, { signed: true })}</MaskedAmount>
        <span className="text-xs text-text-faint">realized</span>
      </span>
    </li>
  );
}

/**
 * Right-rail panel: the wallet's closed positions (zero balance) from the
 * production v3 portfolio snapshot, ranked by served realized PnL. The
 * snapshot carries no buy or sell totals, so no return multiple is shown.
 */
export function TopTrades({ wallet, isOwner, limit = 5, className }: TopTradesProps) {
  const portfolio = usePortfolio(wallet, isOwner);
  const page = portfolio.loadable.state === 'ready' ? portfolio.loadable.data.data : null;
  const trades = useMemo(() => (page ? topTrades(page.positions, limit) : []), [page, limit]);
  const tokens = useMemo(() => trades.map(trade => trade.position.tokenAddress.toLowerCase()), [trades]);
  const metadata = useTokenMetadataBatch(tokens);
  const state = portfolio.loadable;
  const issue = portfolioIssue(state, portfolio.owner);
  const collected = useProfileIssue('top-trades', !issue && state.state === 'error' ? { label: 'Top trades', error: state.error, retry: state.retry } : null);

  return (
    <section aria-labelledby="top-trades-title" className={cn('flex flex-col gap-2 rounded-lg bg-surface-1 p-3', className)}>
      <SectionHeader
        title={<span id="top-trades-title" title="Best closed trades by profit">Top trades</span>}
        level={2}
        count={state.state === 'ready' && trades.length > 0 ? trades.length : undefined}
        action={<span className="text-xs text-text-faint">Profit</span>}
        className="items-center px-1"
      />
      {state.state === 'loading' ? (
        <Skeleton variant="rows" rows={3} label="Loading top trades" />
      ) : issue ? (
        <PortfolioIssueState issue={issue} variant="inline" className="py-2" />
      ) : state.state === 'unsupported' ? (
        <UnavailableState variant="inline" title="Top trades" plural operation={state.operation} status={state.status} message="Check back in a bit" />
      ) : state.state === 'error' && collected ? (
        <PlaceholderRows rows={limit} />
      ) : state.state === 'error' ? (
        <ErrorState variant="inline" title="Can't load top trades right now" message="Can't load top trades right now" onRetry={state.retry} />
      ) : trades.length === 0 ? (
        <EmptyState variant="inline" title="No closed trades yet." body={page?.next ? 'None of the loaded trades are closed yet.' : "Trades show up here once they're sold."} className="py-4 text-center" />
      ) : (
        <>
          <ol aria-label="Top trades" className="flex flex-col gap-0.5">
            {trades.map(trade => {
              const meta = metadata.data?.[trade.position.tokenAddress.toLowerCase()];
              const identity = tokenIdentity(trade.position.tokenAddress, meta);
              return <TradeRow key={trade.position.positionId} trade={trade} name={identity.name} symbol={identity.symbol} logo={identity.logo} poolAddress={meta?.pool_address ?? null} />;
            })}
          </ol>
          {page?.next ? <p className="px-3 text-xs text-text-faint">Based on the first {page.positions.length} of {page.summary.positionCount} positions.</p> : null}
        </>
      )}
    </section>
  );
}
