'use client';
import { useMarketLive } from '@/page-modules/token/market-session-context';
import TradesTable from '@/widgets/coin/trades-table';

/** Compatibility wrapper for callers outside the coin page; the coin page mounts CoinTabs directly. */
export default function TxFeed({ poolAddress }: { poolAddress: string; [legacyPresentationProp: string]: unknown }) {
  const live = useMarketLive(poolAddress);
  return <TradesTable poolAddress={poolAddress} live={live} />;
}
