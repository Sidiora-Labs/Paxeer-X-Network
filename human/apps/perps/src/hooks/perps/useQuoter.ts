import { fetchWithPolicy as fetch } from "@/core/net/fetch";
import { PerpsPrecompileABI } from "@/core/network/abi/PerpsPrecompile";
import { PRECOMPILES } from "@/core/network/precompiles";
import { PERPS_MARKETS } from "@/core/perps/config";
import {
  type MarketQuote,
  type PartialCloseQuote,
  type PrecompileMarketQuote,
  type PrecompilePartialCloseQuote,
  marketQuoteFromPrecompile,
  partialCloseQuoteFromPrecompile,
} from "@/core/perps/quoteFallback";
import { MARKET_DATA_URL as REST_API_URL } from "@/core/feeds";
import { readContract } from "./useContract";

export interface OpenPositionQuote {
  entryPrice: bigint;
  sizeUsd: bigint;
  collateralUsd: bigint;
  leverage: bigint;
  tradingFee: bigint;
  tradingFeeUsd: bigint;
  priceImpact: bigint;
  liquidationPrice: bigint;
  estimatedFunding24h: bigint;
  maintenanceMarginBps: bigint;
}

export interface ClosePositionQuote {
  exitPrice: bigint;
  unrealizedPnl: bigint;
  tradingFee: bigint;
  tradingFeeUsd: bigint;
  fundingOwed: bigint;
  netPnl: bigint;
  estimatedPayout: bigint;
}

export type { MarketQuote, PartialCloseQuote };

// ── Open-interest singleton cache ──────────────────────────────────
interface OiEntry {
  paxeerSymbol: string;
  long_oi: number;
  short_oi: number;
}
let _oiCache: OiEntry[] | null = null;
let _oiLastFetch = 0;
let _oiInflight: Promise<OiEntry[]> | null = null;
const OI_CACHE_TTL = 60_000;

async function _fetchOi(): Promise<OiEntry[]> {
  if (_oiCache && Date.now() - _oiLastFetch < OI_CACHE_TTL) return _oiCache;
  if (_oiInflight) return _oiInflight;
  _oiInflight = fetch(`${REST_API_URL}/open-interest`)
    .then((r) => r.json())
    .then((json) => {
      if (json.success) {
        _oiCache = json.data ?? [];
        _oiLastFetch = Date.now();
      }
      return _oiCache ?? [];
    })
    .catch(() => _oiCache ?? [])
    .finally(() => {
      _oiInflight = null;
    });
  return _oiInflight;
}

function toBigInt18(val: number): bigint {
  return BigInt(Math.round(val * 1e18));
}

function getSymbolForMarket(marketId: number): string {
  return PERPS_MARKETS.find((m) => m.marketId === marketId)?.symbol ?? "BTC";
}

export function useQuoter() {
  /**
   * Fetches market data from the REST /api/ticker/:symbol endpoint.
   * Returns a MarketQuote with values scaled to 18 decimals (bigint)
   * for backward compatibility with consumers that expect bigints.
   */
  async function quoteMarket(marketId: bigint): Promise<MarketQuote> {
    const symbol = getSymbolForMarket(Number(marketId));
    try {
      const res = await fetch(`${REST_API_URL}/ticker/${symbol}`);
      if (!res.ok) throw new Error(`Ticker fetch failed: ${res.status}`);
      const json = await res.json();
      if (!json.success) throw new Error("Ticker response unsuccessful");
      const t = json.data;

      // Also fetch OI breakdown — cached, refreshed at most every 60 s
      let longOI = 0;
      let shortOI = 0;
      try {
        const oiData = await _fetchOi();
        const entry = oiData.find((d) => d.paxeerSymbol === symbol);
        if (entry) {
          longOI = entry.long_oi ?? 0;
          shortOI = entry.short_oi ?? 0;
        }
      } catch {}

      return {
        indexPrice: toBigInt18(t.index_price ?? 0),
        markPrice: toBigInt18(t.mark_price ?? 0),
        oracleTWAP: toBigInt18(t.index_price ?? 0),
        fundingRatePerSecond: BigInt(0),
        fundingRate24h: toBigInt18(t.est_funding_rate ?? 0),
        longOI: toBigInt18(longOI),
        shortOI: toBigInt18(shortOI),
        maxLeverage: BigInt(PERPS_MARKETS.find((m) => m.marketId === Number(marketId))?.maxLeverage ?? 100),
        maintenanceMarginBps: BigInt(1000),
        enabled: true,
        priceStale: false,
      };
    } catch (err) {
      // Fallback to on-chain if REST fails
      console.warn("[useQuoter] REST ticker failed, falling back to on-chain:", err);
      const result = await readContract({
        address: PRECOMPILES.perps,
        abi: PerpsPrecompileABI,
        functionName: "quoteMarket",
        args: [marketId],
      });
      return marketQuoteFromPrecompile(result as PrecompileMarketQuote);
    }
  }

  async function quoteOpenPosition(params: {
    marketId: bigint;
    collateralToken: `0x${string}`;
    collateralAmount: bigint;
    leverage: bigint;
    isLong: boolean;
  }): Promise<OpenPositionQuote> {
    const result = await readContract({
      address: PRECOMPILES.perps,
      abi: PerpsPrecompileABI,
      functionName: "quoteOpenPosition",
      args: [params.marketId, params.collateralToken, params.collateralAmount, params.leverage, params.isLong],
    });
    return result as unknown as OpenPositionQuote;
  }

  async function quoteClosePosition(positionId: bigint): Promise<ClosePositionQuote> {
    const result = await readContract({
      address: PRECOMPILES.perps,
      abi: PerpsPrecompileABI,
      functionName: "quoteClosePosition",
      args: [positionId],
    });
    return result as unknown as ClosePositionQuote;
  }

  async function quotePartialClose(positionId: bigint, closeSizeUsd: bigint): Promise<PartialCloseQuote> {
    const result = await readContract({
      address: PRECOMPILES.perps,
      abi: PerpsPrecompileABI,
      functionName: "quotePartialClose",
      args: [positionId, closeSizeUsd],
    });
    return partialCloseQuoteFromPrecompile(result as PrecompilePartialCloseQuote);
  }

  return { quoteMarket, quoteOpenPosition, quoteClosePosition, quotePartialClose };
}
