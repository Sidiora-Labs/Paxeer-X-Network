"use client";
import { useMemo } from "react";
import { MARKET_DATA_URL } from "@/core/feeds";
import { fetchJsonWithPolicy } from "@/core/net/fetch";
import { useMarketsSnapshot } from "./useMarketsStream";
interface TickerEntry {
  symbol: string;         // "PERP_BTC_USDC"
  paxeerSymbol: string;   // "BTC"
  mark_price: number;
  index_price: number;
  open_interest: number;
  est_funding_rate: number;
  last_funding_rate: number;
  "24h_open": number;
  "24h_close": number;
  "24h_high": number;
  "24h_low": number;
  "24h_amount": number;
  "24h_volume": number;
}

/** Full ticker data keyed by paxeer symbol */
export type TickerMap = Record<string, TickerEntry>;

export interface RealtimePricesResult {
  prices: Record<string, number>;
  tickers: TickerMap;
}


export function useRealtimePrices({ enabled = true }: { enabled?: boolean } = {}): RealtimePricesResult {
  const rows = useMarketsSnapshot(enabled);
  return useMemo(() => {
    const prices: Record<string, number> = {};
    const tickers: TickerMap = {};
    for (const row of rows) {
      if (row.price <= 0 || Date.now() - row.price_updated_at > 30000) continue;
      prices[row.symbol] = row.price;
      tickers[row.symbol] = {
        symbol: `PERP_${row.symbol}_USDC`, paxeerSymbol: row.symbol,
        mark_price: row.price, index_price: row.price,
        open_interest: row.reported_open_interest, est_funding_rate: row.est_funding_rate,
        last_funding_rate: row.funding_8h,
        "24h_open": row.price_24h, "24h_close": row.price,
        "24h_high": Math.max(row.high_24h, row.price), "24h_low": row.low_24h,
        "24h_amount": row.amount_24h, "24h_volume": row.reported_volume_24h,
      };
    }
    return { prices, tickers };
  }, [rows]);
}
// Imperative callers may request a bounded, deduplicated one-shot read.
// No module-level polling and no connections without active subscribers.
let inflight: Promise<TickerMap> | null = null;
export function fetchTickers(): Promise<TickerMap> {
  if (!inflight) inflight = fetchJsonWithPolicy<{ success: boolean; data: TickerEntry[] }>(`${MARKET_DATA_URL}/tickers`)
    .then(body => {
      if (!body.success || !Array.isArray(body.data)) throw new Error("Ticker feed unavailable");
      return Object.fromEntries(body.data.map(row => [row.paxeerSymbol, row]));
    }).finally(() => { inflight = null; });
  return inflight;
}
