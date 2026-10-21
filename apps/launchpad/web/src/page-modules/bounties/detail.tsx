'use client';
import { BountyDetail } from '@/widgets/bounties/bounty-detail';

export default function BountyDetailPage({ bountyId }: { bountyId: string }) {
  return (
    <div className="mx-auto flex w-full max-w-content-narrow-max-width flex-col gap-6 px-4 py-6">
      <BountyDetail bountyId={bountyId} />
    </div>
  );
}
