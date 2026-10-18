import { coinLogoUrl } from "@paxeer-network/paxeer-backend/core/perps/config";
import {
  SPOT_STABLECOIN_LIST,
  SPOT_TOKENS,
  getTradableAddress,
  type SpotTokenMeta,
} from "@paxeer-network/paxeer-backend/core/spot/addresses";

// ── Spot Market Definition ──────────────────────────────────────────
export interface SpotMarket {
  /** Internal symbol used for WS topics and routing (e.g. "SID"). */
  symbol: string;
  /** Human-readable display pair (e.g. "SID/USD"). */
  pair: string;
  /** Full market name. */
  name: string;
  /** Base token display symbol as it appears in the UI. */
  baseToken: string;
  /** Quote side is always unified "USD" (abstraction over USDL/USDC/USDT). */
  quoteToken: "USD";
  /** Token decimals for the base asset. */
  baseDecimals: number;
  /**
   * On-chain ERC-20 address the matching engine actually receives for
   * this market. For native PAX this resolves to `WPAX9`; for every
   * other market it's the base token's own address. Use this when
   * constructing `marketBuy` / `marketSell` calldata.
   */
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
    pair: `${config.symbol}/USD`,
    name: config.name,
    baseToken: meta.symbol,
    quoteToken: "USD",
    baseDecimals: meta.decimals,
    baseAddress: getTradableAddress(meta),
    baseTokenMeta: meta,
    description: config.description,
  };
}

// ── Initial 4 SPOT pairs ────────────────────────────────────────────
export const SPOT_MARKETS: SpotMarket[] = [
  defineSpotMarket(SPOT_TOKENS.SID, {
    symbol: "SID",
    name: "Sidiora Markets",
    description:
      "Sidiora Markets is the decentralized exchange protocol built on Paxeer Network. SID is the governance and utility token that accrues fees from all trading activity on the platform.",
  }),
  defineSpotMarket(SPOT_TOKENS.WBNB, {
    symbol: "BNB",
    name: "Wrapped BNB",
    description:
      "BNB is the native token of the BNB Chain ecosystem, originally created as the utility token for the Binance exchange. It is used for transaction fees, staking, and governance.",
  }),
  defineSpotMarket(SPOT_TOKENS.WETH, {
    symbol: "ETH",
    name: "Wrapped Ether",
    description:
      "Ethereum is the leading smart contract platform powering thousands of decentralized applications, DeFi protocols, and NFTs.",
  }),
  defineSpotMarket(SPOT_TOKENS.PAX, {
    symbol: "PAX",
    name: "Paxeer Network",
    description:
      "Paxeer Network is a next-generation Layer 1 blockchain designed for high-throughput trading applications. Supports both wrapped (WPAX9) and native PAX.",
  }),
];

// ── Stablecoin abstraction ──────────────────────────────────────────
// The three stablecoins are treated as unified "USD" in the SPOT UI.
// The user never picks which stable to buy/sell with — `stableSelector`
// (highest-balance for buys, deepest-vault-liquidity for sells) makes
// the choice deterministically at submit time.
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
  crypto: ["SID", "BNB", "ETH", "PAX"],
};

export const SPOT_CATEGORY_LABELS: Record<string, string> = {
  all: "All Markets",
  crypto: "Crypto",
};
