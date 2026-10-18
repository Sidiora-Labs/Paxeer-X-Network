'use client';

import { useMemo } from 'react';
import Link from 'next/link';
import { Archive, Wallet } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Avatar, Button, EmptyState, ErrorState, LoadMore, Skeleton, UnavailableState } from '@/ui/primitives';
import { formatTokenAmount, formatUsdl, shortAddress } from '@/utils/format';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import type { TokenMetadata } from '@/widgets/home/types';
import { poolPath } from '@/core/app-foundation/navigation';
import { isSignedOutError } from '@/core/market/unsupported';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { MaskedAmount } from './balance-visibility';
import { isOpenPosition, portfolioIssue, positionValueRaw, usePortfolio, usePositionPages, type PortfolioPosition } from './portfolio-transport';
import { PortfolioIssueState } from './portfolio-issue-state';
import { CollectedPlaceholder, useProfileIssue } from './profile-issues';

export interface PositionsOpenClosedProps {
  wallet: string;
  mode: 'open' | 'closed';
  isOwner?: boolean;
  className?: string;
}

export interface TokenIdentity { name: string; symbol: string | null; logo: string | null; known: boolean; decimals: number | null }

export function tokenIdentity(address: string, metadata: TokenMetadata | null | undefined): TokenIdentity {
  if (!metadata) return { name: 'Unknown', symbol: null, logo: null, known: false, decimals: null };
  return {
    name: metadata.name?.trim() || 'Unknown',
    symbol: metadata.symbol?.trim() || null,
    logo: metadata.images?.logo ?? null,
    known: !!metadata.name?.trim(),
    decimals: typeof metadata.decimals === 'number' ? metadata.decimals : null,
  };
}

function signClass(raw: bigint | null): string {
  if (raw === null || raw === 0n) return 'text-text';
  return raw > 0n ? 'text-success' : 'text-danger';
}

function Amount({ raw, signed = false, className }: { raw: bigint | string | null; signed?: boolean; className?: string }) {
  if (raw === null) return <span className="text-text-faint" aria-label="Not available yet">&mdash;</span>;
  const value = typeof raw === 'bigint' ? raw : BigInt(raw);
  return <MaskedAmount className={cn('tabular-nums font-bold text-num-md', signed ? signClass(value) : 'text-text', className)}>{formatUsdl(value, { signed })}</MaskedAmount>;
}

function TokenCell({ address, identity, href }: { address: string; identity: TokenIdentity; href: string }) {
  return (
    <Link href={href} className="flex min-w-0 items-center gap-3 rounded-sm">
      <Avatar src={identity.logo} name={identity.symbol ?? identity.name} size="lg" shape="square" decorative />
      <span className="min-w-0">
        <span className="block truncate text-sm font-bold text-text">{identity.name}{identity.symbol ? <span className="ml-1 font-semibold text-text-muted">{identity.symbol}</span> : null}</span>
        <span className="block truncate font-mono-sl text-xs text-text-faint">{shortAddress(address, 6, 4)}</span>
      </span>
    </Link>
  );
}

/** Coin page link: the launchpad pool when the metadata registry names it, else the token address (the coin route resolves a token to its pool). */
export function coinHref(tokenAddress: string, poolAddress?: string | null): string {
  return poolPath(125, poolAddress && /^0x[0-9a-fA-F]{40}$/.test(poolAddress) ? poolAddress : tokenAddress);
}

function PositionRow({ position, identity, poolAddress, mode, owner }: { position: PortfolioPosition; identity: TokenIdentity; poolAddress: string | null; mode: 'open' | 'closed'; owner: boolean }) {
  const decimals = identity.decimals ?? position.markPrice?.baseAsset.decimals ?? null;
  const valueRaw = positionValueRaw(position);
  const unrealized = position.unrealizedPnlRaw === null ? null : BigInt(position.unrealizedPnlRaw);
  const realized = position.realizedPnlRaw === null ? null : BigInt(position.realizedPnlRaw);
  const href = coinHref(position.tokenAddress, poolAddress);
  const knownRealized = BigInt(position.knownRealizedSubtotalUsdlRaw);
  const dust = valueRaw !== null && valueRaw > 0n && valueRaw < 10_000n;
  return (
    <li className={cn('grid grid-cols-[minmax(0,1fr)_auto] items-center gap-x-4 gap-y-1 rounded-md bg-surface-2 px-3 py-2 sm:min-h-14', mode === 'open' ? 'sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto]' : 'sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)]')}>
      <TokenCell address={position.tokenAddress} identity={identity} href={href} />
      {mode === 'open' ? (
        <>
          <div className="min-w-0 text-right sm:text-left">
            <span className="block text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:hidden">Quantity</span>
            <MaskedAmount className="block truncate text-xs text-text-faint tabular-nums">{decimals !== null ? formatTokenAmount(position.balanceRaw, decimals) : `${position.balanceRaw} raw`}</MaskedAmount>
          </div>
          <div className="min-w-0">
            <span className="block text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:hidden">Value</span>
            <Amount raw={valueRaw} className={cn(dust && 'text-text-faint')} />
            {valueRaw === null ? <span className="block text-2xs text-text-faint">no price yet</span> : null}
          </div>
          <div className="min-w-0 text-right sm:text-left">
            <span className="block text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:hidden">Unrealized</span>
            <Amount raw={unrealized} signed />
          </div>
          {owner ? <Button variant="secondary" size="sm" href={href} className="col-span-2 sm:col-span-1">Sell</Button> : <span aria-hidden="true" className="hidden sm:block sm:w-14" />}
        </>
      ) : (
        <>
          <div className="min-w-0 text-right sm:text-left">
            <span className="block text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:hidden">Realized</span>
            <Amount raw={realized} signed />
          </div>
          <div className="min-w-0">
            <span className="block text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:hidden">Known realized</span>
            <Amount raw={knownRealized} signed />
          </div>
          <div className="min-w-0 text-right sm:text-left">
            <span className="block text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:hidden">Basis</span>
            <span className="text-xs text-text-muted">{position.realizedBasisQuality}</span>
          </div>
        </>
      )}
    </li>
  );
}

/** Empty-tab copy: the owner gets a next step, a visitor gets a neutral sentence about the wallet. */
export function positionsEmptyCopy(mode: 'open' | 'closed', owner: boolean): { title: string; body: string; action: { href: string; label: string } | null } {
  if (mode === 'open') {
    return owner
      ? { title: 'No open positions', body: 'Coins you buy show up here with their value and unrealized profit.', action: { href: '/explore', label: 'Browse coins' } }
      : { title: 'No open positions', body: 'This wallet does not hold any coins right now.', action: null };
  }
  return owner
    ? { title: 'No closed positions yet', body: 'When you sell out of a coin, its realized result lands here.', action: null }
    : { title: 'No closed positions yet', body: 'This wallet has not closed a position yet.', action: null };
}

/**
 * Open (balance above zero) or Closed (zero balance) positions from the
 * production v3 portfolio snapshot (GET /v3/me/portfolio for the signed-in
 * owner, GET /v3/wallets/{wallet}/portfolio for everyone else), token
 * identity from the metadata batch. The snapshot carries no trade count or
 * close time, so closed rows show the realized figures it does carry.
 */
export function PositionsOpenClosed({ wallet, mode, isOwner, className }: PositionsOpenClosedProps) {
  const portfolio = usePortfolio(wallet, isOwner);
  const first = portfolio.loadable.state === 'ready' ? portfolio.loadable.data : null;
  const pages = usePositionPages(first, wallet, portfolio.owner);
  const rows = useMemo(() => pages.positions.filter(position => (mode === 'open' ? isOpenPosition(position) : !isOpenPosition(position))), [pages.positions, mode]);
  const tokens = useMemo(() => rows.map(row => row.tokenAddress.toLowerCase()), [rows]);
  const metadata = useTokenMetadataBatch(tokens);
  const issue = portfolioIssue(portfolio.loadable, portfolio.owner);
  const failed = !issue && portfolio.loadable.state === 'error' && !(portfolio.owner && isSignedOutError(portfolio.loadable.error));
  const label = mode === 'open' ? 'Open positions' : 'Closed positions';
  const collected = useProfileIssue(`positions-${mode}`, failed && portfolio.loadable.state === 'error' ? { label, error: portfolio.loadable.error, retry: portfolio.loadable.retry } : null);

  if (portfolio.loadable.state === 'loading') return <Skeleton variant="rows" rows={4} label={`Loading ${mode} positions`} className={className} />;
  if (issue) return <PortfolioIssueState issue={issue} className={className} />;
  if (portfolio.loadable.state === 'unsupported') return <UnavailableState title={label} plural operation={portfolio.loadable.operation} status={portfolio.loadable.status} message="Check back in a bit" onRetry={portfolio.refetch} className={className} />;
  if (portfolio.loadable.state === 'error') {
    if (portfolio.owner && isSignedOutError(portfolio.loadable.error)) {
      return <SignedOutState title={`Sign in to see your ${mode} positions`} body="Sign in again to see your positions." className={className} />;
    }
    if (collected) return <CollectedPlaceholder label={label} className={className} />;
    return <ErrorState title="Can't load positions right now" message="Check your connection and try again" onRetry={portfolio.loadable.retry} className={className} />;
  }

  if (rows.length === 0 && !pages.hasMore) {
    const copy = positionsEmptyCopy(mode, isOwner ?? portfolio.owner);
    return (
      <EmptyState
        icon={mode === 'open' ? Wallet : Archive}
        title={copy.title}
        body={copy.body}
        action={copy.action ? <Button href={copy.action.href} variant="secondary" size="sm">{copy.action.label}</Button> : undefined}
        className={className}
      />
    );
  }

  return (
    <div className={cn('flex flex-col gap-2', className)}>
      <div className={cn('hidden px-3 text-2xs font-bold uppercase tracking-[.04em] text-text-faint sm:grid sm:gap-x-4', mode === 'open' ? 'sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto]' : 'sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)]')} aria-hidden="true">
        <span>Coin</span>
        {mode === 'open' ? <><span>Quantity</span><span>Value</span><span>Unrealized</span><span className="w-14" /></> : <><span>Realized</span><span>Known realized</span><span>Basis</span></>}
      </div>
      <ul aria-label={mode === 'open' ? 'Open positions' : 'Closed positions'} className="flex flex-col gap-0.5">
        {rows.map(position => (
          <PositionRow key={position.positionId} position={position} mode={mode} owner={portfolio.owner} identity={tokenIdentity(position.tokenAddress, metadata.data?.[position.tokenAddress.toLowerCase()])} poolAddress={metadata.data?.[position.tokenAddress.toLowerCase()]?.pool_address ?? null} />
        ))}
      </ul>
      {metadata.isError ? <p className="px-3 text-xs text-text-faint">Some coin names can't load right now.</p> : null}
      {pages.hasMore || pages.moreError ? <LoadMore onLoadMore={pages.loadMore} hasMore={pages.hasMore} loading={pages.loadingMore} error={!!pages.moreError} auto={false} label="Load more positions" /> : null}
    </div>
  );
}
