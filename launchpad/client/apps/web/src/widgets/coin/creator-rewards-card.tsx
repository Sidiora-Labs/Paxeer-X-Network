'use client';
import type { V2ContractTypes } from '@kindle/sdk';
import { Button, EmptyState, KeyValueRow, Skeleton, UnavailableState } from '@/ui/primitives';
import { SignedOutState } from '@/ui/shared/signed-out-state';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { pnlApiUrl } from '@/core/sdk-config';
import { USDL_ADDRESS } from '@/core/network/contracts';
import { isSignedOutError, toUnsupported } from '@/core/market/unsupported';
import { useViewerWallet } from '@/widgets/social/use-follow';
import { usePrivateAccountQuery } from '@/widgets/profile/use-private-account-query';
import { accountRequest } from '@/widgets/profile/account-transport';
import { formatTokenAmount, formatUsdl } from '@/utils/format';
import { userFacingMessage } from '@/core/user-facing-error';

type RewardsResponse = V2ContractTypes['RewardsResponse'];
type Reward = V2ContractTypes['Reward'];

function readRewards() {
  return accountRequest<RewardsResponse>(pnlApiUrl('/v2/me/rewards?limit=50'));
}

interface ProgramTotals { programId: string; claimable: bigint; earned: bigint; usdl: boolean }

function programTotals(items: readonly Reward[]): ProgramTotals[] {
  const usdl = String(USDL_ADDRESS).toLowerCase();
  const byProgram = new Map<string, ProgramTotals>();
  for (const row of items) {
    const current = byProgram.get(row.programId) ?? { programId: row.programId, claimable: 0n, earned: 0n, usdl: true };
    current.claimable += BigInt(row.claimableRaw);
    current.earned += BigInt(row.earnedRaw);
    current.usdl = current.usdl && row.assetAddress.toLowerCase() === usdl;
    byProgram.set(row.programId, current);
  }
  return [...byProgram.values()];
}

function amount(value: bigint, usdl: boolean): string {
  return usdl ? formatUsdl(value, { compact: true }) : formatTokenAmount(value);
}

/** Shown only to the coin's creator: their reward entitlements from IF_REWARDS (self scoped, every program; the contract carries no per-pool field). */
export default function CreatorRewardsCard({ poolAddress }: { poolAddress: string }) {
  const snapshot = usePoolSnapshot(poolAddress);
  const viewer = useViewerWallet();
  const creator = snapshot.data?.creator?.toLowerCase() ?? null;
  const isCreator = !!viewer && !!creator && viewer === creator;
  const rewards = usePrivateAccountQuery('rewards', readRewards, ['']);
  if (!isCreator) return null;
  const signedOut = !rewards.owner || (rewards.isError && isSignedOutError(rewards.error));
  const marker = rewards.isError && !signedOut ? toUnsupported('IF_REWARDS', rewards.error) : null;
  const totals = rewards.data ? programTotals(rewards.data.data.items) : [];
  return (
    <section aria-labelledby="creator-rewards-title" className="flex flex-col gap-2 rounded-md bg-surface-1 p-3">
      <div className="flex items-center justify-between gap-2">
        <h2 id="creator-rewards-title" className="text-sm font-bold text-text">Your creator rewards</h2>
        <Button variant="link" size="sm" href="/rewards">View rewards</Button>
      </div>
      {signedOut ? <SignedOutState variant="inline" title="Sign in to see your creator rewards" /> : null}
      {rewards.isPending && !signedOut ? <Skeleton variant="rows" rows={2} label="creator rewards" /> : null}
      {marker ? <UnavailableState variant="inline" title="Creator rewards" operation={marker.operation} status={marker.status} /> : null}
      {rewards.isError && !marker && !signedOut ? <p role="alert" className="text-sm text-danger">Creator rewards can&apos;t load right now. {userFacingMessage(rewards.error)}</p> : null}
      {rewards.data ? (
        totals.length ? (
          <dl className="flex flex-col gap-1 rounded-md bg-surface-2 px-3 py-1">
            {totals.map(row => (
              <KeyValueRow key={row.programId} label={row.programId} value={amount(row.claimable, row.usdl)} trailing={<span className="text-xs text-text-faint">claimable</span>} />
            ))}
            {totals.map(row => (
              <KeyValueRow key={`${row.programId}-earned`} label={`${row.programId} earned`} value={amount(row.earned, row.usdl)} />
            ))}
          </dl>
        ) : <EmptyState variant="inline" title="No creator rewards yet" body="Rewards build up as people trade your coin." />
      ) : null}
      <p className="text-xs text-text-faint">Totals include rewards from all your coins.</p>
      <UnavailableState variant="inline" title="Your rewards from this coin" operation="IF_REWARDS.sharePct" status={null} />
    </section>
  );
}
