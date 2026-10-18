'use client';
import { Suspense } from 'react';
import dynamic from 'next/dynamic';
import PremiumErrorBoundary from '@/ui/common/error-boundary';
import { AgeChip, KeyValueRow, ShortAddress, Skeleton } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import TokenDescription from '@/widgets/trading/token-description';

const PoolChat = dynamic(() => import('@/widgets/trading/pool-chat'), { ssr: false });

function explorerAddress(address: string): string | null {
  return safeExternalDestination(`https://paxscan.paxeer.app/address/${address}`);
}

export default function AboutTab({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const data = snapshot.data ?? null;
  const loading = snapshot.isPending;
  return <div className="flex flex-col gap-6">
    <TokenDescription poolAddress={poolAddress} />
    <dl aria-label="Coin details" className="flex flex-col gap-1 rounded-md bg-surface-2 px-4 py-2">
      <KeyValueRow label="Coin address" loading={loading} value={data ? <ShortAddress address={data.pool.tokenAddress} explorerHref={explorerAddress(data.pool.tokenAddress)} className="text-text" /> : null} />
      <KeyValueRow label="Pool address" loading={loading} value={data ? <ShortAddress address={data.pool.poolAddress} explorerHref={explorerAddress(data.pool.poolAddress)} className="text-text" /> : null} />
      <KeyValueRow label="Creator" loading={loading} value={data?.creator ? <ShortAddress address={data.creator} explorerHref={explorerAddress(data.creator)} className="text-text" /> : null} />
      <KeyValueRow label="Created" loading={loading} value={data?.createdAt ? <AgeChip iso={data.createdAt} compact={false} className="text-text" /> : null} />
      <KeyValueRow label="Paired with" value="USDL" />
      <KeyValueRow label="Details version" loading={loading} value={data?.metadata ? data.metadata.revision : null} />
    </dl>
    <section aria-label="Coin chat" className="flex flex-col gap-2">
      <h3 className="text-sm font-bold text-text">Chat</h3>
      <PremiumErrorBoundary area="PoolChat">
        <Suspense fallback={<Skeleton variant="block" height={200} label="chat" />}>
          {data ? <PoolChat poolAddress={data.pool.poolAddress} /> : <p role="status" className="text-sm text-text-faint">Chat opens once this coin finishes loading.</p>}
        </Suspense>
      </PremiumErrorBoundary>
    </section>
  </div>;
}
