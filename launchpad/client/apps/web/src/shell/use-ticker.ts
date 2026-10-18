'use client';

import { useMemo } from 'react';
import type { LucideIcon } from 'lucide-react';
import { History, Star, Trophy, Users, Wallet } from 'lucide-react';
import { useWatchlistRows } from '@/core/market/use-watchlist-rows';
import { useLeaderboard } from '@/core/market/use-leaderboard';
import { useFeed } from '@/core/market/use-feed';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { bpsToPercent } from '@/core/market/market-units';
import { isSignedOutError, toUnsupported, type Unsupported } from '@/core/market/unsupported';
import { useTokenMetadataBatch } from '@/hooks/market/use-token-metadata';
import { readPortfolio } from '@/widgets/profile/account-transport';
import { usePrivateAccountQuery } from '@/widgets/profile/use-private-account-query';
import { formatPercent, formatUsdl, shortAddress } from '@/utils/format';
import { useRecentViewed } from './use-global-search';
import { coinPath } from './paste-address';

export type TickerMode = 'watchlist' | 'earners' | 'holdings' | 'following' | 'recent';

export interface TickerModeConfig {
  id: TickerMode;
  label: string;
  icon: LucideIcon;
  /** Full page for this mode, reachable from the strip by keyboard. */
  href: string;
}

export const tickerModes: readonly TickerModeConfig[] = [
  { id: 'watchlist', label: 'Watchlist', icon: Star, href: '/watchlist' },
  { id: 'earners', label: 'Top earners', icon: Trophy, href: '/leaderboard' },
  { id: 'holdings', label: 'Holdings', icon: Wallet, href: '/profile' },
  { id: 'following', label: 'Following', icon: Users, href: '/leaderboard' },
  { id: 'recent', label: 'Recently viewed', icon: History, href: '/explore' },
];

export function isTickerMode(value: unknown): value is TickerMode {
  return typeof value === 'string' && tickerModes.some(mode => mode.id === value);
}

export type ChipTone = 'up' | 'down' | 'flat' | 'neutral';

export interface TickerChip {
  key: string;
  href: string;
  label: string;
  image: string | null;
  avatarShape: 'round' | 'square';
  value: string | null;
  tone: ChipTone;
}

export type TickerFeed =
  | { state: 'loading' }
  /** `emptyShort` is the narrow-screen variant of `emptyText`, short enough that the open link beside it stays visible. */
  | { state: 'ready'; chips: TickerChip[]; emptyText: string | null; emptyShort?: string }
  | Unsupported
  | { state: 'error'; error: Error; retry(): void }
  | { state: 'signed-out'; prompt: string };

const RAW_INT = /^-?(0|[1-9][0-9]{0,77})$/;


function signTone(raw: string): ChipTone {
  if (raw.startsWith('-')) return 'down';
  return /^0+$/.test(raw) ? 'flat' : 'up';
}

export function percentChip(value: number | null): Pick<TickerChip, 'value' | 'tone'> {
  if (value === null) return { value: null, tone: 'neutral' };
  return { value: formatPercent(value), tone: value > 0 ? 'up' : value < 0 ? 'down' : 'flat' };
}

export function usdlChip(raw: string | null): Pick<TickerChip, 'value' | 'tone'> {
  if (raw === null || !RAW_INT.test(raw)) return { value: null, tone: 'neutral' };
  return { value: formatUsdl(raw, { compact: true, signed: true }), tone: signTone(raw) };
}

function symbolLabel(symbol: string | null | undefined, fallback: string): string {
  return symbol && symbol.trim() ? `$${symbol.trim().toUpperCase()}` : shortAddress(fallback);
}

/** Starred pools joined with their 24h change (IF_STATS_BATCH, complete baselines only) and metadata. */
export function useWatchlistTicker(): TickerFeed {
  const rows = useWatchlistRows();
  if (rows.loading) return { state: 'loading' };
  if (rows.pools.length === 0) return { state: 'ready', chips: [], emptyText: 'Star a coin to follow it here', emptyShort: 'Watchlist is empty' };
  if (rows.stats.state === 'loading') return { state: 'loading' };
  const chips = rows.rows.map<TickerChip>(row => {
    const change = row.stats?.changeBaselineQuality === 'complete' ? bpsToPercent(row.stats.priceChangeBps24h) : null;
    return {
      key: row.poolAddress,
      href: coinPath(row.poolAddress),
      label: symbolLabel(row.metadata?.symbol, row.poolAddress),
      image: row.metadata?.images?.logo ?? null,
      avatarShape: 'square',
      ...percentChip(change),
    };
  });
  return { state: 'ready', chips, emptyText: null };
}

/** The signed-in wallet's token positions from IF_CONNECTED_PORTFOLIO_V3 (`/v3/me/portfolio`) with unrealised PnL. */
export function useHoldingsTicker(): TickerFeed {
  const portfolio = usePrivateAccountQuery('ticker-portfolio', () => readPortfolio());
  const positions = useMemo(() => (portfolio.data?.data.positions ?? []).filter(row => /^[1-9][0-9]*$/.test(row.balanceRaw)), [portfolio.data]);
  const tokens = useMemo(() => positions.map(row => row.tokenAddress.toLowerCase()), [positions]);
  const metadata = useTokenMetadataBatch(tokens);
  if (!portfolio.owner) return { state: 'signed-out', prompt: 'Sign in to see your holdings here' };
  if (portfolio.isPending) return { state: 'loading' };
  if (portfolio.error) {
    if (isSignedOutError(portfolio.error)) return { state: 'signed-out', prompt: 'Sign in to see your holdings here' };
    const marker = toUnsupported('IF_CONNECTED_PORTFOLIO_V3', portfolio.error);
    if (marker) return marker;
    const error = portfolio.error;
    return { state: 'error', error, retry: () => { void portfolio.refetch(); } };
  }
  if (positions.length === 0) return { state: 'ready', chips: [], emptyText: 'No coins held yet', emptyShort: 'No holdings yet' };
  const chips = positions.map<TickerChip>(row => {
    const token = row.tokenAddress.toLowerCase();
    const meta = metadata.data?.[token] ?? null;
    return {
      key: row.positionId,
      href: coinPath(token),
      label: symbolLabel(meta?.symbol, token),
      image: meta?.images?.logo ?? null,
      avatarShape: 'square',
      ...usdlChip(row.unrealizedPnlRaw),
    };
  });
  return { state: 'ready', chips, emptyText: null };
}

/** Coins opened on this device (consent-gated local storage; see use-global-search.ts). */
export function useRecentTicker(): TickerFeed {
  const { recent } = useRecentViewed();
  if (recent.length === 0) return { state: 'ready', chips: [], emptyText: 'Coins you open show up here', emptyShort: 'No recent coins' };
  const chips = recent.map<TickerChip>(item => {
    const mcap = RAW_INT.test(item.marketCap) && !/^0+$/.test(item.marketCap) ? formatUsdl(item.marketCap, { compact: true, unit: false }) : null;
    return {
      key: item.address,
      href: coinPath(item.address),
      label: symbolLabel(item.symbol, item.address),
      image: item.logo,
      avatarShape: 'square',
      value: mcap === null ? null : `MC ${mcap}`,
      tone: 'neutral',
    };
  });
  return { state: 'ready', chips, emptyText: null };
}

/** IF_LEADERBOARDS trader / 24h; the real 404 surfaces as Unsupported until core/api serves it. */
export function useEarnersTicker(): TickerFeed {
  const board = useLeaderboard('trader', '24h', 20);
  const entries = board.state === 'ready' ? board.data.data.snapshot.entries : [];
  const identities = useWalletIdentities(entries.map(entry => entry.wallet));
  if (board.state !== 'ready') return board;
  if (entries.length === 0) return { state: 'ready', chips: [], emptyText: 'No top earners in the last 24h', emptyShort: 'None in 24h' };
  const chips = entries.map<TickerChip>(entry => {
    const wallet = entry.wallet.toLowerCase();
    const signed: unknown = (entry as { metricSigned?: unknown }).metricSigned;
    const metric = usdlChip(entry.quality === 'unavailable' ? null : typeof signed === 'string' ? signed : entry.metricRaw);
    return {
      key: wallet,
      href: `/profile/${wallet}`,
      label: identities.label(wallet),
      image: identities.identities[wallet]?.avatarUrl ?? null,
      avatarShape: 'round',
      ...metric,
    };
  });
  return { state: 'ready', chips, emptyText: null };
}

/** IF_CONNECTED_FEED (chronological); core/api answers 503 until the discovery runtime exists, surfaced as Unsupported. */
export function useFollowingTicker(): TickerFeed {
  const feed = useFeed('chronological', null, 20);
  if (feed.state !== 'ready') return feed;
  const entries = feed.data.Entries ?? [];
  if (entries.length === 0) return { state: 'ready', chips: [], emptyText: 'No callouts from people you follow yet', emptyShort: 'No callouts yet' };
  const chips = entries.map<TickerChip>(card => {
    const pool = card.Source.Pools?.find(value => /^0x[0-9a-fA-F]{40}$/.test(value)) ?? null;
    const body = card.Body.trim();
    return {
      key: card.Source.ID,
      href: pool ? coinPath(pool) : '/',
      label: body.length > 48 ? `${body.slice(0, 47)}…` : body,
      image: null,
      avatarShape: 'round',
      value: null,
      tone: 'neutral',
    };
  });
  return { state: 'ready', chips, emptyText: null };
}
