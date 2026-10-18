'use client';
import Link from 'next/link';
import dynamic from 'next/dynamic';
import { Avatar, ShortAddress, Skeleton, UnavailableState } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useWalletIdentities } from '@/core/market/use-wallet-identities';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { FollowButton } from '@/widgets/social/follow-button';
import { shortAddress } from '@/utils/format';
import { formatShare, statsShare } from '@/core/market/pool-stats-display';
import { StatDash } from '@/ui/shared/stat-value';

const GoLiveEntry = dynamic(() => import('@/widgets/live/go-live-entry'), { ssr: false });

/** Creator attribution: avatar, display name linked to the profile, wallet, follow control and the creator-only broadcast entry. */
export default function CreatorCard({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const creator = snapshot.data?.creator?.toLowerCase() ?? null;
  const identities = useWalletIdentities(creator ? [creator] : []);
  const identity = creator ? identities.identities[creator] : undefined;
  const name = identity?.displayName?.trim() || (creator ? shortAddress(creator) : '');
  const avatar = identity?.avatarUrl ?? null;
  const stats = snapshot.data?.stats ?? null;
  // PoolStats.creatorHoldingShareBps: the creator wallet's share of total supply (pump.fun's dev holding).
  const share = stats ? statsShare(stats, 'creator') : null;
  return (
    <section aria-labelledby="creator-card-title" className="flex flex-col gap-3 rounded-md bg-surface-1 p-3">
      <h2 id="creator-card-title" className="text-sm font-bold text-text">Creator</h2>
      {snapshot.isPending ? <Skeleton variant="rows" rows={2} label="creator" /> : null}
      {snapshot.isSuccess && !creator ? <UnavailableState variant="inline" title="Creator info" operation={snapshot.data.unavailable.registry?.operation ?? 'IF_POOL_DETAIL'} status={snapshot.data.unavailable.registry?.status ?? null} /> : null}
      {snapshot.isError ? <UnavailableState variant="chip" wrap title="Creator info" plural="auto" operation="IF_POOL_STATS" status={null} onRetry={() => { void snapshot.refetch(); }} className="self-start" /> : null}
      {creator ? (
        <div className="flex items-center gap-3">
          <Link href={`/profile/${creator}`} className="shrink-0 rounded-pill focus-visible:outline-2 focus-visible:outline-accent" aria-label={`${name} profile`}>
            <Avatar src={avatar} name={name} size="lg" decorative loading={identity?.resolution === 'pending'} />
          </Link>
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <Link href={`/profile/${creator}`} className="truncate text-sm font-bold text-text hover:underline">{name}</Link>
            <ShortAddress address={creator} explorerHref={safeExternalDestination(`https://paxscan.paxeer.app/address/${creator}`)} />
            {share ? (
              <span data-creator-share className="inline-flex items-center gap-1 text-xs text-text-faint">
                Holds {share.value === null ? <StatDash reason={share.reason ?? 'Not available yet'} subject="Creator share" align="start" /> : <span data-numeric className="font-bold tabular-nums text-text-muted">{formatShare(share.value)}</span>} of supply
              </span>
            ) : null}
          </div>
          <FollowButton wallet={creator} handle={identity?.displayName ?? undefined} size="sm" />
        </div>
      ) : null}
      {creator ? <GoLiveEntry poolAddress={poolAddress} className="w-full" /> : null}
    </section>
  );
}
