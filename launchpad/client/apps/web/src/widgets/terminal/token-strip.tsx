'use client';
import type { ReactNode } from 'react';
import { AtSign, Bell, Globe, Link as LinkIcon, MessageCircle, Send, Settings, Share2, Star } from 'lucide-react';
import { toast } from 'sonner';
import { AgeChip, Avatar, DropdownMenu, IconButton, Skeleton, StageBadge, StatTile, Tooltip, UnavailableState } from '@/ui/primitives';
import CopyAddress from '@/ui/shared/copy-address';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import type { PoolStats, Token } from '@/core/market/pool-resolver';
import { formatSpotPrice, statsSpotPrice } from '@/core/market/pool-stats-display';
import { useTokenMetadata } from '@/hooks/market/use-token-metadata';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { useWatchlist } from '@/hooks/ui/use-watchlist';
import { priceNumber } from '@/page-modules/token/market-presentation';
import { safeSocialUrl } from '@/widgets/trading/token-description';
import { stageFor } from '@/widgets/explore/use-explore-rows';
import { formatPrice, formatTokenAmount, formatUsdl } from '@/utils/format';
import { useTerminalLive } from './live-context';
import { terminalOpenPanelEvent, terminalResetColumnsEvent } from './status-bar/model';

export interface StripStat {
  key: 'ath' | 'price' | 'liquidity' | 'supply' | 'fees' | 'curve';
  label: string;
  /** Visible value; null renders "-". */
  value: string | null;
  /** Screen-reader unit spoken after the value. */
  unit?: string;
  /** Why the value is missing (or qualified); shown as a tooltip and spoken. */
  reason: string | null;
}

export interface StripStatInput {
  stats: PoolStats | null;
  token: Token | null;
  livePrice: number | null;
}

/** Maps the served pool reads onto the six strip tiles; anything the backend does not serve stays null with the reason. */
export function tokenStripStats({ stats, token, livePrice }: StripStatInput): StripStat[] {
  // PoolStats.spotPriceWad (exact, 1e18-scaled) is the last known price until the live feed quotes; null means no trade yet.
  const spot = stats ? statsSpotPrice(stats).value : null;
  const spotPrice = spot !== null && Number(spot) > 0 ? spot : null;
  const price = livePrice !== null && Number.isFinite(livePrice) ? formatPrice(livePrice) : spotPrice !== null ? formatSpotPrice(spotPrice) : null;
  const priceReason = livePrice !== null ? null : spotPrice !== null ? 'Last known price, live updates are catching up' : 'No price yet. A price shows up after the first trade.';

  const liquidity = stats ? formatUsdl(stats.realUsdlBalanceRaw, { compact: true, unit: false }) : null;
  const liquidityReason = stats ? null : 'Liquidity is not available for this coin yet';

  let supply: string | null = null;
  let supplyReason: string | null;
  if (!token) supplyReason = 'Coin details are not available yet';
  else if (token.totalSupplyRaw === null) supplyReason = 'Supply is not available yet';
  else if (token.decimalsQuality !== 'verified' || token.decimals === null) supplyReason = 'Supply is not available yet';
  else {
    supply = formatTokenAmount(token.totalSupplyRaw, token.decimals, { compact: true });
    supplyReason = token.supplyQuality === 'verified' ? null : 'Supply may not be final yet';
  }

  return [
    { key: 'ath', label: 'All time high', value: null, reason: 'All-time high is not available yet' },
    { key: 'price', label: 'Price', value: price, reason: priceReason },
    { key: 'liquidity', label: 'Liquidity', value: liquidity, unit: 'USDL', reason: liquidityReason },
    { key: 'supply', label: 'Supply', value: supply, reason: supplyReason },
    { key: 'fees', label: 'Total fees paid', value: null, reason: 'Total fees paid is not available yet' },
    { key: 'curve', label: 'Bonding curve', value: null, reason: 'Bonding curve progress is not available yet' },
  ];
}

function socialIcon(label: string, url: string) {
  const key = `${label} ${url}`.toLowerCase();
  if (/x\.com|twitter/.test(key)) return AtSign;
  if (/t\.me|telegram/.test(key)) return Send;
  if (/discord/.test(key)) return MessageCircle;
  if (/web|site|home/.test(key)) return Globe;
  return LinkIcon;
}

function socialHref(url: string): string | null {
  return safeExternalDestination(url) ?? safeSocialUrl(url);
}

async function shareCurrentPage(title: string) {
  const url = window.location.href;
  try {
    if (typeof navigator.share === 'function') { await navigator.share({ title, url }); return; }
    await navigator.clipboard.writeText(url);
    toast.success('Link copied');
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') return;
    toast.error("Couldn't share this link, try again");
  }
}

function StripTile({ stat }: { stat: StripStat }) {
  let value: ReactNode = stat.value;
  if (stat.value === null) {
    value = (
      <Tooltip content={stat.reason ?? 'Not available yet'} align="end">
        <span tabIndex={0} aria-label={`Not available yet. ${stat.reason ?? ''}`} className="cursor-help text-text-faint">-</span>
      </Tooltip>
    );
  } else if (stat.reason) {
    value = (
      <Tooltip content={stat.reason} align="end">
        <span tabIndex={0} className="cursor-help">{stat.value}{stat.unit ? <span className="sr-only"> {stat.unit}</span> : null}<span className="sr-only">. {stat.reason}</span></span>
      </Tooltip>
    );
  }
  return (
    <StatTile
      label={stat.label}
      value={value}
      unit={stat.value !== null && !stat.reason ? stat.unit : undefined}
      size="row"
      className="min-w-16 [&>dd]:text-sm"
    />
  );
}

/** Terminal coin identity strip: avatar, symbol, actions, socials and six pool stat tiles. */
export default function TerminalTokenStrip({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const data = snapshot.data ?? null;
  const live = useTerminalLive(poolAddress);
  const watchlist = useWatchlist();
  const image = useTokenMetadata(data?.pool.tokenAddress ?? null);

  if (snapshot.isPending) {
    return (
      <header aria-busy="true" className="flex min-h-13 items-center gap-3 rounded-md bg-surface-1 px-3 py-1">
        <Skeleton variant="block" width={44} height={44} label="coin image" />
        <div className="flex flex-1 flex-col gap-1"><Skeleton variant="text" width={140} /><Skeleton variant="text" width={200} /></div>
      </header>
    );
  }
  if (snapshot.isError || !data) {
    const status = (snapshot.error as { status?: unknown } | null)?.status;
    return (
      <header className="rounded-md bg-surface-1 p-2">
        <UnavailableState variant="inline" title="This coin" operation="IF_POOL_DETAIL · IF_SEARCH" status={typeof status === 'number' ? status : null} onRetry={() => { void snapshot.refetch(); }} />
      </header>
    );
  }

  const symbol = data.metadata?.displaySymbol ?? data.token?.symbol ?? 'Coin';
  const name = data.metadata?.displayName ?? data.token?.name ?? '';
  const pool = data.pool.poolAddress;
  const starred = watchlist.check(pool);
  const createdMs = data.createdAt ? Date.parse(data.createdAt) : NaN;
  const stage = stageFor(Number.isFinite(createdMs) ? live.now - createdMs : null);
  const socials = (data.metadata?.socialLinks ?? []).map(link => ({ ...link, href: socialHref(link.url) })).filter(link => link.href !== null);
  const stats = tokenStripStats({ stats: live.state.stats ?? data.stats, token: data.token, livePrice: priceNumber(live.state.price) });

  return (
    <header aria-label={`${symbol} summary`} className="flex min-h-13 flex-wrap items-center gap-x-4 gap-y-2 rounded-md bg-surface-1 px-3 py-1">
      <div className="flex min-w-0 flex-1 items-center gap-2.5">
        <Avatar src={image.data?.images?.logo ?? null} name={symbol} size="lg" shape="square" decorative className="size-11" badge={stage ? <StageBadge stage={stage} size={14} surface={1} /> : undefined} />
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex min-w-0 items-center gap-1.5">
            <h1 className="truncate text-sm font-extrabold text-text">{symbol}</h1>
            {name ? <span className="truncate text-xs text-text-muted">{name}</span> : null}
            <CopyAddress address={data.pool.tokenAddress} variant="icon" />
            <IconButton label={starred ? 'Remove from watchlist' : 'Add to watchlist'} pressed={starred} icon={<Star className={starred ? 'fill-current text-accent' : undefined} />} size="sm" className="size-6" onClick={() => watchlist.toggle(pool)} />
            <IconButton label="Share coin link" icon={<Share2 />} size="sm" className="size-6" onClick={() => { void shareCurrentPage(`${symbol} on Sidiora`); }} />
            <IconButton label="Price alerts" icon={<Bell />} size="sm" className="size-6" onClick={() => window.dispatchEvent(new CustomEvent(terminalOpenPanelEvent, { detail: 'alerts' }))} />
          </div>
          <div className="flex min-w-0 items-center gap-2">
            <AgeChip iso={data.createdAt} now={live.now} className="font-bold text-accent" />
            {socials.length ? (
              <ul aria-label="Links" className="flex items-center gap-0.5">
                {socials.map(link => {
                  const Icon = socialIcon(link.label, link.url);
                  return (
                    <li key={link.url}>
                      <a href={link.href as string} target="_blank" rel="noopener noreferrer" aria-label={link.label} className="inline-flex size-5 items-center justify-center rounded-xs text-text-faint transition-colors hover:bg-surface-hover hover:text-text">
                        <Icon aria-hidden="true" className="size-3.5" />
                      </a>
                    </li>
                  );
                })}
              </ul>
            ) : null}
          </div>
        </div>
      </div>
      <dl className="flex flex-wrap items-center gap-x-4 gap-y-1">
        {stats.map(stat => <StripTile key={stat.key} stat={stat} />)}
      </dl>
      <DropdownMenu
        label="Terminal settings"
        size="sm"
        align="end"
        trigger={<Settings aria-hidden="true" className="size-icon-sm" />}
        items={[{ id: 'reset-layout', label: 'Reset layout', onSelect: () => window.dispatchEvent(new CustomEvent(terminalResetColumnsEvent)) }]}
      />
    </header>
  );
}
