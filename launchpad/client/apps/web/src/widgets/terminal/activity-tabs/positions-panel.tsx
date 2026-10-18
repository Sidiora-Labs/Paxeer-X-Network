'use client';
import { useSyncExternalStore } from 'react';
import { LogIn, Wallet } from 'lucide-react';
import { Button, EmptyState, ErrorState, LoadMore, SortHeader, Skeleton, UnavailableState } from '@/ui/primitives';
import { cn } from '@/lib/utils';
import { formatTokenAmount, formatUsdl } from '@/utils/format';
import { openAuthModal } from '@/shell/auth-modal-store';
import { isSignedOutError } from '@/core/market/unsupported';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { accountSession, subscribeAccount } from '@/widgets/profile/account-transport';
import { positionValueRaw, usePortfolio, usePositionPages, type PortfolioPosition } from '@/widgets/profile/portfolio-transport';
import type { FillsToken } from './trades-panel';
import { userFacingMessage } from '@/core/user-facing-error';

const noSession = () => null;

/** Positions of the portfolio that hold this pool's token (case-insensitive address match). */
export function positionsForToken(positions: readonly PortfolioPosition[], tokenAddress: string): PortfolioPosition[] {
  const target = tokenAddress.toLowerCase();
  return positions.filter(position => position.tokenAddress.toLowerCase() === target);
}

function signedTone(raw: string | null): string {
  if (raw === null) return 'text-text-faint';
  const value = BigInt(raw);
  return value > 0n ? 'text-success' : value < 0n ? 'text-danger' : 'text-text';
}

function QualityTag({ label, value, good }: { label: string; value: string; good: string }) {
  if (value === good) return null;
  return <span className="rounded-pill bg-surface-3 px-1.5 py-0.5 text-2xs font-bold text-warning">{label}</span>;
}

export interface PositionsTableProps { positions: readonly PortfolioPosition[]; token: FillsToken; symbol: string }

/** Columns Token | Balance | Remaining cost | Realized | Unrealized | Mark value, with basis / pricing quality tags. */
export function PositionsTable({ positions, token, symbol }: PositionsTableProps) {
  const decimals = token.decimalsQuality === 'verified' ? token.decimals : null;
  return (
    <div className="overflow-x-auto">
      <table aria-label="Your positions in this pair" className="w-full min-w-[640px] text-xs [&_td]:whitespace-nowrap [&_td]:px-3">
        <thead className="bg-surface-1">
          <tr>
            <SortHeader className="h-6" label="Token" />
            <SortHeader className="h-6" label="Balance" align="right" />
            <SortHeader className="h-6" label="Remaining cost" align="right" />
            <SortHeader className="h-6" label="Realized" align="right" />
            <SortHeader className="h-6" label="Unrealized" align="right" />
            <SortHeader className="h-6" label="Mark value" align="right" />
          </tr>
        </thead>
        <tbody>
          {positions.map(position => {
            const value = positionValueRaw(position);
            return (
              <tr key={position.positionId} data-position-id={position.positionId} className="h-9 bg-surface-1 transition-colors hover:bg-surface-hover">
                <td>
                  <span className="inline-flex items-center gap-1.5">
                    <span className="font-bold text-text">{symbol}</span>
                    <QualityTag label="Cost incomplete" value={position.basisQuality} good="complete" />
                    <QualityTag label="No price yet" value={position.pricingQuality} good="priced" />
                  </span>
                </td>
                <td data-numeric className="text-right tabular-nums text-text">{decimals === null ? <span className="text-text-faint" title="Not available yet">-</span> : formatTokenAmount(position.balanceRaw, decimals)}</td>
                <td data-numeric className="text-right tabular-nums text-text">{position.remainingCostUsdlRaw === null ? <span className="text-text-faint" title={`At least ${formatUsdl(position.knownRemainingCostUsdlRaw)}, some buys are missing`}>-</span> : formatUsdl(position.remainingCostUsdlRaw)}</td>
                <td data-numeric className={cn('text-right tabular-nums', signedTone(position.realizedPnlRaw))}>{position.realizedPnlRaw === null ? <span title="Not available yet">-</span> : formatUsdl(position.realizedPnlRaw, { signed: true })}</td>
                <td data-numeric className={cn('text-right tabular-nums', signedTone(position.unrealizedPnlRaw))}>{position.unrealizedPnlRaw === null ? <span title="No price yet">-</span> : formatUsdl(position.unrealizedPnlRaw, { signed: true })}</td>
                <td data-numeric className="text-right tabular-nums text-text">{value === null ? <span className="text-text-faint" title="No price yet">-</span> : formatUsdl(value)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function SignedInPositions({ wallet, tokenAddress, token, symbol }: { wallet: string; tokenAddress: string; token: FillsToken; symbol: string }) {
  const read = usePortfolio(wallet);
  const first = read.loadable.state === 'ready' ? read.loadable.data : null;
  const pages = usePositionPages(first, wallet, read.owner);
  if (read.loadable.state === 'loading') return <Skeleton variant="rows" rows={3} label="positions" />;
  if (read.loadable.state === 'unsupported') return <UnavailableState title="Positions" operation={read.loadable.operation} status={read.loadable.status} message={read.loadable.message} onRetry={read.refetch} />;
  if (read.loadable.state === 'error' && isSignedOutError(read.loadable.error)) return <SignedOutState icon={LogIn} title="Sign in to see your position" body="Your session ended. Sign in again to read your positions." />;
  if (read.loadable.state === 'error') return <ErrorState variant="inline" title="Positions can't load right now" message={userFacingMessage(read.loadable.error)} onRetry={read.loadable.retry} />;
  const matching = positionsForToken(pages.positions, tokenAddress);
  return (
    <div className="flex flex-col gap-1">
      {matching.length
        ? <PositionsTable positions={matching} token={token} symbol={symbol} />
        : <EmptyState icon={Wallet} title={pages.hasMore ? 'No position in this pair on the loaded pages' : 'No position yet'} body={pages.hasMore ? 'Your portfolio has more pages; load them to finish the search.' : 'Buy from the trade panel to open one.'} />}
      {pages.hasMore || pages.moreError
        ? <LoadMore auto={false} hasMore={pages.hasMore} loading={pages.loadingMore} error={pages.moreError !== null} label="Load more portfolio positions" onLoadMore={pages.loadMore} />
        : null}
    </div>
  );
}

/** Positions tab: session-gated; the signed-in portfolio filtered to this pool's token. */
export function PositionsPanel({ tokenAddress, token, symbol }: { tokenAddress: string; token: FillsToken; symbol: string }) {
  const scope = useSyncExternalStore(subscribeAccount, accountSession, noSession);
  if (!scope) {
    return <EmptyState icon={LogIn} title="Sign in to see your position" body="Positions come from your signed-in portfolio." action={<Button size="sm" onClick={() => openAuthModal('Sign in to see your positions')}>Sign in</Button>} />;
  }
  return <SignedInPositions wallet={scope.session.wallet} tokenAddress={tokenAddress} token={token} symbol={symbol} />;
}
