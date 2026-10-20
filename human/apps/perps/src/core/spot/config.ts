import { coinLogoUrl } from "@/core/perps/config";
import {
  SPOT_STABLECOIN_LIST,
  SPOT_TOKENS,
  type SpotTokenMeta,
} from "@/core/spot/addresses";

// ── Spot Market Definition ──────────────────────────────────────────
export interface SpotMarket {
  /** Internal symbol used for WS topics and routing (e.g. "SID"). */
  symbol: string;
  /** Human-readable display pair (e.g. "SID/USDL"). */
  pair: string;
  /** Full market name. */
  name: string;
  /** Base token display symbol as it appears in the UI. */
  baseToken: string;
  /** Quote side is always USDL, the only stable token of the venue. */
  quoteToken: "USDL";
  /** Token decimals for the base asset. */
  baseDecimals: number;
  /** On-chain address of the base token entry. */
  baseAddress: `0x${string}`;
  /** Full base-token metadata from the address registry. */
  baseTokenMeta: SpotTokenMeta;
  /** Optional description. */
  description?: string;
}

/** Build a `SpotMarket` from a token registry entry + display config. */
function defineSpotMarket(
  meta: SpotTokenMeta,
  config: {
    symbol: string;
    name: string;
    description?: string;
  },
): SpotMarket {
  return {
    symbol: config.symbol,
    pair: `${config.symbol}/USDL`,
    name: config.name,
    baseToken: meta.symbol,
    quoteToken: "USDL",
    baseDecimals: meta.decimals,
    baseAddress: meta.address,
    baseTokenMeta: meta,
    description: config.description,
  };
}

// ── Opening pairs of the spot venue ─────────────────────────────────
export const SPOT_MARKETS: SpotMarket[] = [
  defineSpotMarket(SPOT_TOKENS.PAX, {
    symbol: "PAX",
    name: "Paxeer Network",
    description:
      "Paxeer Network is a next-generation Layer 1 blockchain designed for high-throughput trading applications.",
  }),
  defineSpotMarket(SPOT_TOKENS.SID, {
    symbol: "SID",
    name: "Sidiora Markets",
    description:
      "Sidiora Markets is the decentralized exchange protocol built on Paxeer Network. SID is the governance and utility token that accrues fees from all trading activity on the platform.",
  }),
];

// ── Stablecoin abstraction ──────────────────────────────────────────
// USDL is the only stable token; `stableSelector` settles every trade in it.
//
// `StablecoinMeta` is an alias for the unified token registry shape so
// every consumer (orders, balances, indexer reconciliation) speaks the
// same types. Re-exported as `SPOT_STABLECOINS` for backward compat
// with existing widgets that import the list directly.
export type StablecoinMeta = SpotTokenMeta;

export const SPOT_STABLECOINS: readonly StablecoinMeta[] = SPOT_STABLECOIN_LIST;

// ── Helpers ─────────────────────────────────────────────────────────

/** Set of SPOT symbols for quick lookup. */
export const SPOT_SYMBOLS = new Set(SPOT_MARKETS.map((m) => m.symbol));

/** Look up a spot market by symbol. */
export function getSpotMarket(symbol: string): SpotMarket | undefined {
  return SPOT_MARKETS.find((m) => m.symbol === symbol.toUpperCase());
}

/** Reuse the existing coin logo CDN. */
export function spotCoinLogoUrl(symbol: string): string {
  return coinLogoUrl(symbol);
}

/** Default spot symbol when none is selected. */
export const DEFAULT_SPOT_SYMBOL = "SID";

// ── Spot categories ─────────────────────────────────────────────────
export const SPOT_CATEGORIES: Record<string, string[]> = {
  crypto: ["PAX", "SID"],
};

export const SPOT_CATEGORY_LABELS: Record<string, string> = {
  all: "All Markets",
  crypto: "Crypto",
};
