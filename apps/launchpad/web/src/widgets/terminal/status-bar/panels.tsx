'use client';
import { useMemo } from 'react';
import Link from 'next/link';
import { Flame, LineChart, Rss, Star } from 'lucide-react';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { cn } from '@/lib/utils';
import { useRankings } from '@/core/market/use-rankings';
import { usePoolListings } from '@/core/market/pool-listings';
import { statsByPool, useStatsBatch } from '@/core/market/use-stats-batch';
import { bpsPercent } from '@/page-modules/token/market-presentation';
import { useFeed } from '@/core/market/use-feed';
import { isSignedOutError, loadable, type Loadable } from '@/core/market/unsupported';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { useWatchlistTicker, type TickerChip } from '@/shell/use-ticker';
import { openAuthModal } from '@/shell/auth-modal-store';
import { Avatar, Button, EmptyState, ErrorState, KeyValueRow, PercentPill, Skeleton, UnavailableState } from '@/ui/primitives';
import { formatUsdl, shortAddress } from '@/utils/format';
import { readPortfolio } from '@/widgets/profile/account-transport';
import { sumUnrealized } from '@/widgets/profile/portfolio-transport';
import { useAccountSession } from '@/widgets/profile/use-account-session';
import { usePrivateAccountQuery } from '@/widgets/profile/use-private-account-query';
import { terminalPoolPath } from '../sub-nav';
import { userFacingMessage } from '@/core/user-facing-error';

const rowClass = 'flex h-10 items-center gap-2 rounded-sm px-2 text-sm transition-colors duration-fast hover:bg-surface-hover focus-visible:bg-surface-hover';

function Loading({ label }: { label: string }) {
  return <Skeleton variant="rows" rows={4} label={label} />;
}

function LoadableState<T>({ value, title, signedOut }: { value: Exclude<Loadable<T>, { state: 'ready' }>; title: string; signedOut?: string }) {
  if (value.state === 'loading') return <Loading label={title} />;
  if (value.state === 'unsupported') return <UnavailableState variant="panel" title={title} operation={value.operation} status={value.status} message={value.message} className="p-4" />;
  if (signedOut && isSignedOutError(value.error)) return <SignedOutState title={signedOut} />;
  return <ErrorState variant="panel" title={`${title} can't load right now`} message={userFacingMessage(value.error)} onRetry={value.retry} className="p-4" />;
}

function ChipRow({ chip }: { chip: TickerChip }) {
  return (
    <li>
      <Link href={terminalPoolPath(chip.key)} className={rowClass}>
        <Avatar src={chip.image} name={chip.label} size="sm" shape={chip.avatarShape} decorative />
        <span className="min-w-0 flex-1 truncate font-bold text-text">{chip.label}</span>
        <span className={cn('text-xs tabular-nums', chip.tone === 'up' ? 'text-percent-up' : chip.tone === 'down' ? 'text-percent-down' : 'text-text-muted')}>{chip.value ?? '-'}</span>
      </Link>
    </li>
  );
}

export function WatchlistPanel() {
  const feed = useWatchlistTicker();
  if (feed.state === 'loading') return <Loading label="watchlist" />;
  if (feed.state === 'signed-out') return <SignedOutState icon={Star} title="Sign in to see your watchlist" body={feed.prompt} />;
  if (feed.state !== 'ready') return <LoadableState value={feed} title="Watchlist" signedOut="Sign in to see your watchlist" />;
  if (feed.chips.length === 0) return <EmptyState variant="panel" icon={Star} title="Your watchlist is empty" body="Star a coin to follow it here." action={<Button variant="secondary" size="sm" href="/explore">Browse coins</Button>} />;
  return (
    <>
      <p className="px-2 pb-1 text-2xs text-text-faint">24h change</p>
      <ul className="flex flex-col gap-0.5">{feed.chips.map(chip => <ChipRow key={chip.key} chip={chip} />)}</ul>
    </>
  );
}

export function TrendingPanel() {
  const rankings = useRankings('trending', null, 10);
  const items = useMemo(() => rankings.data?.data.snapshot.items ?? [], [rankings.data]);
  const pools = useMemo(() => items.map(item => item.poolAddress.toLowerCase()), [items]);
  // Token identity from the pool listings (IF_POOL_LIST sort=trending, IF_POOL_DETAIL for misses); 24h change from IF_STATS_BATCH.
  const listings = usePoolListings(pools, 'trending');
  const stats = useStatsBatch(pools);
  const byPool = useMemo(() => statsByPool(stats.data), [stats.data]);
  const tokens = useMemo(() => pools.map(pool => listings.data?.[pool]?.tokenAddress ?? null).filter((value): value is string => value !== null), [pools, listings.data]);
  const metadata = useTokenMetadataBatch(tokens);
  const state = loadable(rankings, 'IF_RANKINGS');
  if (state.state !== 'ready') return <LoadableState value={state} title="Trending" />;
  if (items.length === 0) return <EmptyState variant="panel" icon={Flame} title="No coins trending yet" body="Rankings appear as soon as coins trade." />;
  return (
    <ol className="flex flex-col gap-0.5">
      {items.map(item => {
        const pool = item.poolAddress.toLowerCase();
        const token = listings.data?.[pool]?.tokenAddress ?? null;
        const meta = token ? metadata.data?.[token] ?? null : null;
        const label = meta?.symbol ? `$${meta.symbol.toUpperCase()}` : shortAddress(pool);
        const row = byPool.get(pool) ?? null;
        const change = row && row.changeBaselineQuality === 'complete' ? bpsPercent(row.priceChangeBps24h) : null;
        return (
          <li key={pool}>
            <Link href={terminalPoolPath(pool)} className={rowClass}>
              <span className="w-5 shrink-0 text-xs tabular-nums text-text-faint">{item.rank}</span>
              <Avatar src={meta?.images?.logo ?? null} name={label} size="sm" shape="square" decorative />
              <span className="min-w-0 flex-1 truncate font-bold text-text">{label}</span>
              <PercentPill value={change} variant="text" />
            </Link>
          </li>
        );
      })}
    </ol>
  );
}

export function FeedPanel() {
  const feed = useFeed('chronological', null, 20);
  if (feed.state !== 'ready') return <LoadableState value={feed} title="Feed" />;
  const entries = feed.data.Entries ?? [];
  if (entries.length === 0) return <EmptyState variant="panel" icon={Rss} title="No posts yet" body="Callouts and updates appear here as people post them." />;
  return (
    <ul className="flex flex-col gap-0.5">
      {entries.map(card => {
        const pool = card.Source.Pools?.find(value => /^0x[0-9a-fA-F]{40}$/.test(value)) ?? null;
        const body = <span className="line-clamp-2 text-sm text-text">{card.Body}</span>;
        return (
          <li key={card.Source.ID}>
            {pool ? <Link href={terminalPoolPath(pool)} className={cn(rowClass, 'h-auto py-2')}>{body}</Link> : <div className="px-2 py-2">{body}</div>}
          </li>
        );
      })}
    </ul>
  );
}

function signedTone(raw: string | null): string {
  if (raw === null) return 'text-text-faint';
  return raw.startsWith('-') ? 'text-percent-down' : /^0+$/.test(raw) ? 'text-text' : 'text-percent-up';
}

function PnlSummary({ wallet }: { wallet: string }) {
  const portfolio = usePrivateAccountQuery('portfolio-summary', () => readPortfolio(), [], wallet);
  const value = loadable(portfolio, 'IF_CONNECTED_PORTFOLIO_V3');
  if (value.state !== 'ready') return <LoadableState value={value} title="PnL tracker" signedOut="Sign in to track your PnL" />;
  const { summary, positions, next } = value.data.data;
  const unrealized = sumUnrealized(positions, Boolean(next));
  const realizedRaw = summary.realizedPnlRaw ?? summary.knownRealizedSubtotalUsdlRaw;
  const valueRaw = summary.completeValueUsdlRaw ?? summary.pricedSubtotalUsdlRaw;
  const unrealizedRaw = unrealized.raw.toString();
  return (
    <div className="flex flex-col px-2">
      <dl>
        <KeyValueRow label={summary.completeValueUsdlRaw === null ? 'Holdings value (priced coins only)' : 'Holdings value'} value={formatUsdl(valueRaw, { compact: true })} />
        <KeyValueRow label={summary.realizedPnlRaw === null ? 'Realized PnL (partial)' : 'Realized PnL'} value={<span className={signedTone(realizedRaw)}>{formatUsdl(realizedRaw, { signed: true })}</span>} />
        <KeyValueRow label={unrealized.partial ? `Unrealized PnL (${unrealized.counted} coins priced)` : 'Unrealized PnL'} value={<span className={signedTone(unrealizedRaw)}>{formatUsdl(unrealizedRaw, { signed: true })}</span>} />
        <KeyValueRow label="Positions" value={String(summary.positionCount)} />
      </dl>
      {summary.unpricedAssetCount > 0 ? <p className="py-1 text-xs text-text-faint">{summary.unpricedAssetCount} position{summary.unpricedAssetCount === 1 ? '' : 's'} without a mark price are left out of the value.</p> : null}
      <Button variant="secondary" size="sm" href="/terminal/portfolio" className="mt-2">Open portfolio</Button>
    </div>
  );
}

export function PnlPanel() {
  const { session } = useAccountSession();
  const wallet = session?.session.wallet ?? null;
  if (!wallet) {
    return <EmptyState variant="panel" icon={LineChart} title="Track your PnL" body="Sign in to see your holdings value and realized and unrealized PnL."
      action={<Button variant="primary" size="sm" onClick={() => openAuthModal()}>Sign in</Button>} />;
  }
  return <PnlSummary wallet={wallet} />;
}

/** Panels whose backing read does not exist in the contract: no request is made, the gap is named. */
export function MissingOperationPanel({ title, operation, message }: { title: string; operation: string; message: string }) {
  return <UnavailableState variant="panel" title={title} operation={operation} status={null} message={message} className="p-4" />;
}
