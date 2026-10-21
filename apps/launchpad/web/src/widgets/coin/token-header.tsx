'use client';
import { useState, type ReactNode } from 'react';
import { ArrowLeftRight, AtSign, Coins, Globe, Link as LinkIcon, MessageCircle, Send, Share2, Star } from 'lucide-react';
import { toast } from 'sonner';
import { AgeChip, Avatar, IconButton, ShortAddress, Skeleton, Tooltip, UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import type { PoolStats } from '@/core/market/pool-resolver';
import { formatShare, formatSpotPrice, statsChange, statsCirculatingMarketCap, statsMarketCap, statsShare, statsSpotPrice, type StatValue } from '@/core/market/pool-stats-display';
import { StatChange, StatDash } from '@/ui/shared/stat-value';
import { useTokenMetadata } from '@/hooks/market/use-token-metadata';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { useWatchlist } from '@/hooks/ui/use-watchlist';
import { ReportMenu } from '@/widgets/social/report-menu';
import { safeSocialUrl } from '@/widgets/trading/token-description';
import { formatCount, formatPrice, formatUsdl, shortAddress } from '@/utils/format';

type Headline = 'mcap' | 'price';

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

export interface TokenHeaderProps {
  poolAddress: string;
  /** Latest stats from the page's single live market session; the snapshot row is the fallback. */
  liveStats: PoolStats | null;
  /** Live quote price in USDL per token, already reduced to a number. */
  livePrice: number | null;
  now: number;
  /** Compact status chip (market freshness) shown on the identity row; never a page-wide banner. */
  notice?: ReactNode;
}

const headerClass = 'flex flex-col gap-3 rounded-md bg-surface-1 p-3 lg:flex-row lg:items-center lg:gap-4';

function explorerHref(address: string): string | null {
  return safeExternalDestination(`https://paxscan.paxeer.app/address/${address}`);
}

/** One compact header stat: faint label over a bold value, no tile (pump.fun's 24h vol / Holders / ATH cluster). */
function HeaderStat({ label, value, unit }: { label: string; value: string | null; unit?: string }) {
  return (
    <div className="flex min-w-0 flex-col">
      <dt className="text-2xs font-bold uppercase tracking-[.04em] text-text-faint">{label}</dt>
      <dd data-numeric className={`truncate text-sm font-bold tabular-nums ${value ? 'text-text' : 'text-text-faint'}`}>
        {value === null ? <span aria-label="No data">—</span> : <>{value}{unit ? <span className="ml-1 text-xs font-normal text-text-faint">{unit}</span> : null}</>}
      </dd>
    </div>
  );
}

/** Headline valuation (market cap or price, toggled) plus the 24h volume and holder count; every value may be null and renders as a dash. */
function ValuationCluster({ headline, onToggle, value, reason, secondary, change, volume24h, holders }: { headline: Headline; onToggle: () => void; value: string | null; reason: string | null; secondary?: string | null; change: StatValue<number> | null; volume24h: string | null; holders: string | null }) {
  const label = headline === 'mcap' ? 'Market cap' : 'Price';
  return (
    <div className="flex flex-wrap items-end gap-x-5 gap-y-2 lg:justify-end">
      <div className="flex items-end gap-2">
        <button
          type="button"
          onClick={onToggle}
          aria-label={`${label} ${value ?? `not available yet${reason ? `: ${reason}` : ''}`}. Switch to ${headline === 'mcap' ? 'price' : 'market cap'}`}
          title={value === null ? reason ?? undefined : secondary ?? undefined}
          className="-mx-1 flex flex-col items-start rounded-sm px-1 text-left transition-colors hover:bg-surface-hover"
        >
          <span className="flex items-center gap-1 text-2xs font-bold uppercase tracking-[.04em] text-text-faint">{label}<ArrowLeftRight aria-hidden="true" className="size-3" /></span>
          <span data-numeric data-headline={headline} className={`text-xl font-extrabold tabular-nums ${value ? 'text-text' : 'text-text-faint'}`}>{value ?? '—'}</span>
        </button>
        {change ? <span data-change="24h" className="pb-1"><StatChange change={change} variant="text" size="sm" align="start" decimals={2} /></span> : null}
      </div>
      <dl className="flex items-end gap-5">
        <HeaderStat label="24h vol" value={volume24h} unit="USDL" />
        <HeaderStat label="Holders" value={holders} />
      </dl>
    </div>
  );
}

export default function TokenHeader({ poolAddress, liveStats, livePrice, now, notice }: TokenHeaderProps) {
  const snapshot = usePoolSnapshot(poolAddress);
  const watchlist = useWatchlist();
  const [headline, setHeadline] = useState<Headline>('mcap');
  const data = snapshot.data ?? null;
  const token = data?.token ?? null;
  const stats = liveStats ?? data?.stats ?? null;
  const image = useTokenMetadata(data?.pool.tokenAddress ?? null);
  const logo = image.data?.images?.logo ?? null;

  if (snapshot.isPending) {
    return (
      <header data-coin-header="loading" aria-busy="true" className="flex min-h-[72px] items-center gap-3 rounded-md bg-surface-1 p-3">
        <Skeleton variant="block" width={64} height={64} label="coin image" />
        <div className="flex flex-1 flex-col gap-2"><Skeleton variant="text" width={160} /><Skeleton variant="text" width={240} /></div>
      </header>
    );
  }
  if (snapshot.isError || !data) {
    // The snapshot failed but the URL already names the pool: keep the header frame (identity row,
    // explorer link, watch/share, valuation tiles) and degrade only the values to dashes.
    const status = (snapshot.error as { status?: unknown } | null)?.status;
    const starredByUrl = watchlist.check(poolAddress);
    return (
      <header data-coin-header="degraded" aria-label="Coin header" className={headerClass}>
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <span aria-hidden="true" className="flex size-avatar-2xl shrink-0 items-center justify-center rounded-md bg-surface-2 text-text-faint"><Coins className="size-6" /></span>
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
              <h1 className="truncate text-lg font-extrabold text-text"><span className="sr-only">Coin </span>{shortAddress(poolAddress)}</h1>
              {notice}
            </div>
            <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
              <ShortAddress address={poolAddress} explorerHref={explorerHref(poolAddress)} onCopied={() => toast.success('Address copied')} />
              <div className="flex items-center gap-0.5">
                <IconButton label={starredByUrl ? 'Remove from watchlist' : 'Add to watchlist'} pressed={starredByUrl} icon={<Star className={starredByUrl ? 'fill-current text-accent' : undefined} />} size="sm" onClick={() => watchlist.toggle(poolAddress)} />
                <IconButton label="Share coin link" icon={<Share2 />} size="sm" onClick={() => { void shareCurrentPage('Coin on Sidiora'); }} />
              </div>
              <UnavailableState variant="chip" wrap title="This coin" plural="auto" operation="IF_POOL_DETAIL" status={typeof status === 'number' ? status : null} onRetry={() => { void snapshot.refetch(); }} />
            </div>
          </div>
        </div>
        <ValuationCluster headline={headline} onToggle={() => setHeadline(current => (current === 'mcap' ? 'price' : 'mcap'))} value={null} reason={null} change={null} volume24h={null} holders={null} />
      </header>
    );
  }

  const symbol = data.metadata?.displaySymbol ?? token?.symbol ?? 'Coin';
  const name = data.metadata?.displayName ?? token?.name ?? '';
  const pool = data.pool.poolAddress;
  const starred = watchlist.check(pool);
  // Market cap is price x total supply (pump.fun's figure); the circulating valuation is the secondary value on hover.
  const mcapStat = statsMarketCap(stats);
  const circulating = statsCirculatingMarketCap(stats).value;
  const mcap = mcapStat.value === null ? null : formatUsdl(mcapStat.value, { compact: true });
  // The live quote first; else PoolStats.spotPriceWad (null before the first trade).
  const spot = statsSpotPrice(stats);
  const price = livePrice !== null && Number.isFinite(livePrice) ? formatPrice(livePrice) : spot.value === null ? null : formatSpotPrice(spot.value);
  const change24h = stats ? statsChange(stats, '24h') : null;
  const volume24h = stats ? formatUsdl(stats.volume24hRaw, { compact: true, unit: false }) : null;
  const holders = stats?.holderCount !== null && stats?.holderCount !== undefined ? formatCount(stats.holderCount) : null;
  const devShare = stats ? statsShare(stats, 'creator') : null;
  const socials = (data.metadata?.socialLinks ?? []).map(link => ({ ...link, href: socialHref(link.url) })).filter(link => link.href !== null);
  const headlineValue = headline === 'mcap' ? mcap : price;
  const headlineReason = headline === 'mcap' ? mcapStat.reason : livePrice !== null ? null : spot.reason;

  return (
    <header data-coin-header="ready" aria-label={`${symbol} header`} className={headerClass}>
      <div className="flex min-w-0 flex-1 items-center gap-3">
        <Avatar src={logo} name={symbol} size="2xl" shape="square" decorative />
        <div className="flex min-w-0 flex-1 flex-col gap-1">
          <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
            <h1 className="truncate text-lg font-extrabold text-text">{symbol}</h1>
            {name ? <span className="truncate text-sm text-text-muted">{name}</span> : null}
            {data.createdAt ? <AgeChip iso={data.createdAt} now={now} /> : null}
            {devShare ? (
              devShare.value !== null ? (
                <Tooltip content="How much of the supply the creator holds">
                  <span tabIndex={0} data-dev-share className="rounded-pill bg-surface-2 px-2 py-0.5 text-xs font-bold tabular-nums text-text-muted">Creator {formatShare(devShare.value)}</span>
                </Tooltip>
              ) : (
                <span data-dev-share className="inline-flex items-center gap-1 rounded-pill bg-surface-2 px-2 py-0.5 text-xs font-bold text-text-faint">Creator <StatDash reason={devShare.reason ?? 'Not available yet'} subject="Creator share" align="start" /></span>
              )
            ) : null}
            {notice}
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1">
            <ShortAddress address={data.pool.tokenAddress} explorerHref={explorerHref(data.pool.tokenAddress)} onCopied={() => toast.success('Address copied')} />
            {socials.length ? (
              <ul aria-label="Links" className="flex items-center gap-1">
                {socials.map(link => {
                  const Icon = socialIcon(link.label, link.url);
                  return (
                    <li key={link.url}>
                      <a href={link.href as string} target="_blank" rel="noopener noreferrer" aria-label={link.label} className="inline-flex size-6 items-center justify-center rounded-xs text-text-faint transition-colors hover:bg-surface-hover hover:text-text">
                        <Icon aria-hidden="true" className="size-icon-sm" />
                      </a>
                    </li>
                  );
                })}
              </ul>
            ) : null}
            <div className="flex items-center gap-0.5">
              <IconButton label={starred ? 'Remove from watchlist' : 'Add to watchlist'} pressed={starred} icon={<Star className={starred ? 'fill-current text-accent' : undefined} />} size="sm" onClick={() => watchlist.toggle(pool)} />
              <IconButton label="Share coin link" icon={<Share2 />} size="sm" onClick={() => { void shareCurrentPage(`${symbol} on Sidiora`); }} />
              <ReportMenu subject={{ targetType: 'token', targetId: data.pool.tokenAddress, label: symbol }} size="sm" />
            </div>
          </div>
        </div>
      </div>
      <ValuationCluster headline={headline} onToggle={() => setHeadline(current => (current === 'mcap' ? 'price' : 'mcap'))} value={headlineValue} reason={headlineReason} secondary={headline === 'mcap' && circulating ? `Circulating market cap ${formatUsdl(circulating, { compact: true })}` : null} change={change24h} volume24h={volume24h} holders={holders} />
    </header>
  );
}
