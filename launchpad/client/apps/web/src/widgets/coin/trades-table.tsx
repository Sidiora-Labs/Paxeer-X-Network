'use client';
import TabUnavailable from './tab-unavailable';
import { useEffect, useState } from 'react';
import { ErrorState, LoadMore, Skeleton } from '@/ui/primitives';
import { usePoolSnapshot } from '@/core/market/use-pool-snapshot';
import { useMarketTrades, type useMarketLive } from '@/page-modules/token/market-session-context';
import { MarketTradeFeed, type TradeFeedPool, type TradeFeedToken } from '@/page-modules/token/market-trade-feed';
import { safeExternalDestination } from '@/core/app-foundation/navigation';
import { userFacingMessage } from '@/core/user-facing-error';

/** The coin page's single live market session; owned by the page so the trades tab never opens a second stream. */
export type MarketLive = ReturnType<typeof useMarketLive>;

const explorerOrigin = safeExternalDestination('https://paxscan.paxeer.app/')?.replace(/\/$/, '');

function TradesLive({ pool, token, live }: { pool: TradeFeedPool; token: TradeFeedToken | null; live: MarketLive }) {
  const [cursor, setCursor] = useState<string | null>(null);
  const trades = useMarketTrades(pool.poolAddress, cursor);
  useEffect(() => { setCursor(null); }, [pool.poolAddress]);
  useEffect(() => { if (trades.data) live.session.mergePage(trades.data.data.items, trades.data.revision); }, [trades.data, live.session]);
  const watermark = live.state.watermark ?? trades.data?.watermark ?? null;
  return <div className="flex flex-col gap-2">
    {trades.isError && live.state.rows.length === 0 ? <ErrorState title="Trades can't load right now" message={userFacingMessage(trades.error)} onRetry={() => { void trades.refetch(); }} retrying={trades.isFetching} /> : null}
    {trades.isError && live.state.rows.length > 0 ? <ErrorState variant="inline" message="Older trades can't load right now. Newer ones are still shown." /> : null}
    {trades.isError && live.state.rows.length === 0 ? null : trades.isPending && live.state.rows.length === 0 ? <Skeleton variant="rows" rows={6} label="trades" /> : <MarketTradeFeed pool={pool} token={token} rows={live.state.rows} watermark={watermark} contiguous={live.state.stream === 'live'} explorerOrigin={explorerOrigin} />}
    {trades.data ? <LoadMore auto={false} hasMore={trades.data.nextCursor !== null} loading={trades.isFetching} error={trades.isError} label="Load older trades" endLabel="No older trades" onLoadMore={() => { if (trades.data?.nextCursor) setCursor(trades.data.nextCursor); }} /> : null}
  </div>;
}

export default function TradesTable({ poolAddress, live }: { poolAddress: string; live: MarketLive }) {
  const snapshot = usePoolSnapshot(poolAddress);
  if (snapshot.isPending) return <Skeleton variant="rows" rows={6} label="trades" />;
  if (snapshot.isError) return <TabUnavailable title="Trades" error={snapshot.error} onRetry={() => { void snapshot.refetch(); }} />;
  const token = snapshot.data.token;
  if (!token) return <ErrorState title="Trades can't load right now" message="Something went wrong, try again" onRetry={() => { void snapshot.refetch(); }} />;
  return <TradesLive pool={snapshot.data.pool} token={token} live={live} />;
}
