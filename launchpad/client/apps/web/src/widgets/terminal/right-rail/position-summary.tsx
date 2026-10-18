'use client';
import type { ReactNode } from 'react';
import { Button, Tooltip } from '@/ui/primitives';
import { isSignedOutError, type Loadable } from '@/core/market/unsupported';
import { openAuthModal } from '@/shell/auth-modal-store';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { portfolioIssue, positionValueRaw, usePortfolio, type PortfolioPosition } from '@/widgets/profile/portfolio-transport';
import { formatUsdl } from '@/utils/format';
import { cn } from '@/lib/utils';
import { userFacingMessage } from '@/core/user-facing-error';

export interface SummaryFigure { raw: bigint | null; note: string; partial: boolean }
export interface PositionFigures { value: SummaryFigure; remaining: SummaryFigure; realized: SummaryFigure; totalPnl: SummaryFigure }

const figure = (raw: bigint | null, note: string, partial = false): SummaryFigure => ({ raw, note, partial });

/**
 * Honest mapping of the production v3 portfolio position (GET /v3/me/portfolio)
 * onto the four summary figures. Value is the held balance at the served mark
 * price; Remaining is the remaining cost basis (known-quantity basis only,
 * marked partial, when part of the balance has no basis); Realized is the
 * served realized PnL (known subtotal, marked partial, when the realized basis
 * is incomplete); Total PNL is realized + unrealized when both are served,
 * else the served half marked partial. The snapshot carries no lifetime buy
 * or sell totals, so none are shown.
 */
export function positionFigures(position: PortfolioPosition | null, reason: string): PositionFigures {
  if (!position) return { value: figure(null, reason), remaining: figure(null, reason), realized: figure(null, reason), totalPnl: figure(null, reason) };
  const valueRaw = positionValueRaw(position);
  const value = valueRaw !== null
    ? figure(valueRaw, 'What your coins are worth at the current price')
    : figure(null, 'No price yet');
  const remaining = position.remainingCostUsdlRaw !== null
    ? figure(BigInt(position.remainingCostUsdlRaw), 'What you paid for the coins you still hold')
    : figure(BigInt(position.knownRemainingCostUsdlRaw), 'Some of your buys are missing, so this is a minimum', true);
  const realized = position.realizedPnlRaw !== null
    ? figure(BigInt(position.realizedPnlRaw), 'Profit or loss from what you sold')
    : figure(BigInt(position.knownRealizedSubtotalUsdlRaw), 'Some of your sells are missing, so this is partial', true);
  const realizedRaw = position.realizedPnlRaw === null ? null : BigInt(position.realizedPnlRaw);
  const unrealized = position.unrealizedPnlRaw === null ? null : BigInt(position.unrealizedPnlRaw);
  const totalPnl = realizedRaw !== null && unrealized !== null
    ? figure(realizedRaw + unrealized, 'Sold plus unsold profit or loss')
    : realizedRaw !== null
      ? figure(realizedRaw, 'Sold only, no price yet for what you hold', true)
      : unrealized !== null
        ? figure(unrealized, 'Unsold only, some sells are missing', true)
        : figure(null, 'Not available yet');
  return { value, remaining, realized, totalPnl };
}

function reasonOf<T>(state: Loadable<T>): string {
  if (state.state === 'loading') return 'Loading...';
  if (state.state === 'unsupported') return 'Your position is not available yet';
  if (state.state === 'error') return isSignedOutError(state.error) ? 'Sign in to read your position' : `Your position can't load right now. ${userFacingMessage(state.error)}`;
  return '';
}

function Tile({ label, value, tone, note, partial }: { label: string; value: ReactNode; tone?: 'up' | 'down'; note: string; partial: boolean }) {
  return (
    <div className="flex min-w-0 flex-col items-center gap-0.5">
      <dt className="text-2xs text-text-faint">{label}</dt>
      <dd className={cn('max-w-full truncate text-xs font-bold tabular-nums', tone === 'up' ? 'text-success' : tone === 'down' ? 'text-danger' : 'text-text')}>
        <Tooltip content={note}>
          <span tabIndex={0} aria-label={`${label}: ${typeof value === 'string' ? value : ''}${partial ? ' (partial)' : ''}. ${note}`}>
            {value}{partial ? <span className="text-text-faint">*</span> : null}
          </span>
        </Tooltip>
      </dd>
    </div>
  );
}

const toneOf = (raw: bigint | null): 'up' | 'down' | undefined => (raw === null || raw === 0n ? undefined : raw > 0n ? 'up' : 'down');

function Tiles({ figures, loading }: { figures: PositionFigures; loading: boolean }) {
  const show = (item: SummaryFigure, signed = false) => (loading && item.raw === null ? '...' : item.raw === null ? '-' : formatUsdl(item.raw, { compact: true, unit: false, signed }));
  return (
    <dl className="grid grid-cols-4 gap-1">
      <Tile label="Value" value={show(figures.value)} note={figures.value.note} partial={figures.value.partial} />
      <Tile label="Remaining" value={show(figures.remaining)} note={figures.remaining.note} partial={figures.remaining.partial} />
      <Tile label="Realized" value={show(figures.realized, true)} tone={toneOf(figures.realized.raw)} note={figures.realized.note} partial={figures.realized.partial} />
      <Tile label="Total PNL" value={show(figures.totalPnl, true)} tone={toneOf(figures.totalPnl.raw)} note={figures.totalPnl.note} partial={figures.totalPnl.partial} />
    </dl>
  );
}

function OwnedSummary({ wallet, tokenAddress }: { wallet: string; tokenAddress: string | null }) {
  const portfolio = usePortfolio(wallet, true);
  const book = portfolio.loadable.state === 'ready' ? portfolio.loadable.data : null;
  const issue = portfolioIssue(portfolio.loadable, true);
  const position = book && tokenAddress ? book.data.positions.find(row => row.tokenAddress.toLowerCase() === tokenAddress) ?? null : null;
  const reason = !tokenAddress
    ? 'Loading...'
    : book
      ? (book.data.next ? 'No position found yet' : 'You don\'t hold this coin')
      : issue ? issue.title : reasonOf(portfolio.loadable);
  const figures = positionFigures(position, reason);
  return (
    <>
      <Tiles figures={figures} loading={portfolio.loadable.state === 'loading'} />
      {issue?.kind === 'signed-out' || issue?.kind === 'reauth' ? (
        <div className="flex justify-center">
          <Button variant="link" size="sm" onClick={() => openAuthModal('terminal-position')}>{issue.kind === 'reauth' ? 'Sign in again to see your position' : 'Sign in to see your position'}</Button>
        </div>
      ) : issue ? (
        <p role="status" className="text-center text-2xs text-text-faint">{issue.title}. {issue.body}</p>
      ) : null}
    </>
  );
}

const signedOut: PositionFigures = {
  value: figure(null, 'Sign in to read your position'),
  remaining: figure(null, 'Sign in to read your position'),
  realized: figure(null, 'Sign in to read your position'),
  totalPnl: figure(null, 'Sign in to read your position'),
};

/** Own position in this pool's token from the production v3 portfolio: Value / Remaining / Realized / Total PNL. */
export function PositionSummary({ tokenAddress }: { poolAddress: string; tokenAddress: string | null }) {
  const { session } = useAccountSession();
  const wallet = session?.session.wallet.toLowerCase() ?? null;
  return (
    <section aria-label="Your position" className="flex flex-col gap-1 rounded-md bg-surface-1 px-2 py-2">
      {wallet ? <OwnedSummary wallet={wallet} tokenAddress={tokenAddress} /> : (
        <>
          <Tiles figures={signedOut} loading={false} />
          <div className="flex justify-center">
            <Button variant="link" size="sm" onClick={() => openAuthModal('terminal-position')}>Sign in to see your position</Button>
          </div>
        </>
      )}
    </section>
  );
}
