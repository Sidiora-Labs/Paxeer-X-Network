'use client';
import HoldersTable from '@/widgets/coin/holders-table';

/** Compatibility wrapper for callers outside the coin page; the coin page mounts CoinTabs directly. */
export default function HoldersPanel({ poolAddress }: { poolAddress: string; [legacyPresentationProp: string]: unknown }) {
  return <HoldersTable poolAddress={poolAddress} />;
}
