import { MARKET_DATA_URL } from "./core/feeds";
import { fetchJsonWithPolicy } from "./core/net/fetch";
import type { MarketSnapshot } from "./hooks/perps/useMarketsStream";

interface FeedTicker {
  paxeerSymbol: string;
  symbol: string;
  mark_price: number;
  open_interest: number;
  est_funding_rate: number;
  last_funding_rate: number;
  updated_at: number;
  "24h_open": number;
  "24h_close": number;
  "24h_high": number;
  "24h_low": number;
  "24h_amount": number;
  "24h_volume": number;
}
type FeedMarket = Omit<
  MarketSnapshot,
  "volume_7d" | "volume_30d" | "trades_count"
>;
let pending: Promise<FeedMarket[]> | null = null;
export function readAuthenticatedFeedMarkets(): Promise<FeedMarket[]> {
  if (!pending)
    pending = fetchJsonWithPolicy<{ success: boolean; data: FeedTicker[] }>(
      `${MARKET_DATA_URL}/tickers`,
    )
      .then((body) => {
        if (!body.success || !Array.isArray(body.data))
          throw new Error("Ticker feed unavailable");
        const now = Date.now();
        return body.data
          .filter(
            (row) =>
              Number.isFinite(row.mark_price) &&
              row.mark_price > 0 &&
              Number.isFinite(row.updated_at) &&
              now - row.updated_at <= 30_000 &&
              row.updated_at <= now + 5_000,
          )
          .map((row) => ({
            symbol: row.paxeerSymbol,
            symbol_full: row.symbol,
            price: row.mark_price,
            price_updated_at: row.updated_at,
            last_updated: row.updated_at,
            change_24h:
              row["24h_open"] > 0
                ? (row.mark_price / row["24h_open"] - 1) * 100
                : 0,
            volume_24h: row["24h_volume"],
            reported_volume_24h: row["24h_volume"],
            open_interest: row.open_interest,
            reported_open_interest: row.open_interest,
            funding_8h: row.last_funding_rate,
            est_funding_rate: row.est_funding_rate,
            price_24h: row["24h_open"],
            high_24h: row["24h_high"],
            low_24h: row["24h_low"],
            amount_24h: row["24h_amount"],
          }));
      })
      .finally(() => {
        pending = null;
      });
  return pending;
}
