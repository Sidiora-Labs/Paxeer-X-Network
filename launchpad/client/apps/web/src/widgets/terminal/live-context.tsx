'use client';
import { createContext, useContext, type ReactNode } from 'react';
import { useMarketLive } from '@/page-modules/token/market-session-context';
import type { MarketLive } from '@/widgets/coin/trades-table';

const TerminalLiveContext = createContext<{ pool: string; live: MarketLive } | null>(null);

/** One market session for the open pair, shared by every terminal panel that needs live fills, price or stream state. */
export function TerminalLiveProvider({ poolAddress, children }: { poolAddress: string; children: ReactNode }) {
  const pool = poolAddress.toLowerCase();
  const live = useMarketLive(pool);
  return <TerminalLiveContext.Provider value={{ pool, live }}>{children}</TerminalLiveContext.Provider>;
}

/** The shared live session for `poolAddress`, or null outside a TerminalLiveProvider for that pool. */
export function useOptionalTerminalLive(poolAddress: string | null): MarketLive | null {
  const value = useContext(TerminalLiveContext);
  if (!value || poolAddress === null || value.pool !== poolAddress.toLowerCase()) return null;
  return value.live;
}

/** The shared live session for `poolAddress`; throws when no TerminalLiveProvider for that pool is mounted above. */
export function useTerminalLive(poolAddress: string): MarketLive {
  const live = useOptionalTerminalLive(poolAddress);
  if (!live) throw new Error(`TerminalLiveProvider for ${poolAddress.toLowerCase()} is not mounted`);
  return live;
}
