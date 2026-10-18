import { formatUnits } from "viem";
import { QuoterFacetABI } from "./core/network/abi/QuoterFacet";
import { PERPS_MARKETS, DIAMOND_ADDRESS } from "./core/perps/config";
import { readContract } from "./hooks/perps/useContract";
import type { MarketQuote } from "./hooks/perps/useQuoter";
/** Public on-chain fallback. Never supplies synthetic candles or 24-hour statistics. */
export async function readNativeChainMarkets() {
  const rows: any[] = [];
  let index = 0;
  await Promise.all(
    Array.from({ length: 3 }, async () => {
      while (index < PERPS_MARKETS.length) {
        const market = PERPS_MARKETS[index++];
        try {
          const quote = (await readContract({
            address: DIAMOND_ADDRESS,
            abi: QuoterFacetABI,
            functionName: "quoteMarket",
            args: [BigInt(market.marketId)],
          })) as unknown as MarketQuote;
          if (!quote.enabled || quote.priceStale || quote.markPrice <= 0n)
            continue;
          rows.push({
            symbol: market.symbol,
            price: Number(formatUnits(quote.markPrice, 18)),
            index_price: Number(formatUnits(quote.indexPrice, 18)),
            open_interest: Number(
              formatUnits(quote.longOI + quote.shortOI, 18),
            ),
            est_funding_rate:
              Number(formatUnits(quote.fundingRatePerSecond, 18)) * 28800,
            last_updated: Date.now(),
            price_updated_at: Date.now(),
            maintenance_margin_bps: Number(quote.maintenanceMarginBps),
            max_leverage: Number(formatUnits(quote.maxLeverage, 18)),
          });
        } catch {
          /* An unavailable or stale market remains unavailable. */
        }
      }
    }),
  );
  return rows;
}
