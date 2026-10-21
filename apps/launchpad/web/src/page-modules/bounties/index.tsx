'use client';
import { Suspense } from 'react';
import BountiesHome, { BountiesHomeSkeleton } from '@/widgets/bounties/bounties-home';

export default function BountiesPage() {
  return (
    <Suspense fallback={<BountiesHomeSkeleton />}>
      <BountiesHome />
    </Suspense>
  );
}
