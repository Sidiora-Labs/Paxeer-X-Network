import { gqlEndpoint, type GraphQLEndpoint } from "@paxeer-network/paxeer-backend/core/graphql/client";

// ── Chain (re-export from canonical core location for back-compat) ───
// The chain object lives at @paxeer-network/paxeer-backend/core/chain. Re-exported here so existing
// `import { paxeerChain } from "./config"` consumers keep working until
// they're individually migrated to import from @paxeer-network/paxeer-backend/core/chain.
export { paxeerChain } from "@paxeer-network/paxeer-backend/core/chain";

// ── Contract addresses ───────────────────────────────────────────────
export const DIAMOND_ADDRESS =
  "0xFEf3751eFA8d861C54A58A8041a2F8814BbBA623" as `0x${string}`;

// ── Unified Indexer API ──────────────────────────────────────────────
// Subgraph (positions/orders/etc.) is hard-routed through the dedicated
// HTTPS proxy at perps.sidiora.xyz — see
// /etc/nginx/sites-available/perps.sidiora.xyz.https.conf which proxies:
//   https://perps.sidiora.xyz/api/subgraph → http://185.218.126.215:8080/graphql
//   wss://perps.sidiora.xyz/indexer-ws     → ws://185.218.126.215:8080/graphql
//
// Everything ELSE (REST stats, order-flow WS, daily PnL, tickers, …) keeps
// using the legacy us-east-1 indexer host. Don't move them through the
// new proxy without coordination.
export const INDEXER_BASE_URL =
  process.env.NEXT_PUBLIC_INDEXER_URL ||
  "https://us-east-1.perpetual-protocol.sidiora.exchange";

// Subgraph HTTP + WS go through the same-IP proxy:
export const SUBGRAPH_PROXY_BASE = "https://perps.sidiora.xyz";
export const GRAPHQL_URL = `${SUBGRAPH_PROXY_BASE}/api/subgraph`;
export const SUBGRAPH_WS_URL = "wss://perps.sidiora.xyz/indexer-ws";

// Legacy indexer endpoints (REST + order-flow WS) stay on us-east-1.
export const REST_API_URL = `${INDEXER_BASE_URL}/api`;
export const WS_URL =
  process.env.NEXT_PUBLIC_INDEXER_WS_URL ||
  "wss://us-east-1.perpetual-protocol.sidiora.exchange/ws";

// Backward compat alias — consumers that still import SUBGRAPH_URL
export const SUBGRAPH_URL = GRAPHQL_URL;

/**
 * Singleton GraphQL endpoint descriptor for the unified Sidiora indexer.
 * Subgraph HTTP queries + graphql-transport-ws subscriptions are hard
 * routed through the perps.sidiora.xyz nginx proxy so the browser never
 * touches the upstream IP:port directly and wss:// works from HTTPS.
 */
export const INDEXER_GRAPHQL: GraphQLEndpoint = gqlEndpoint(GRAPHQL_URL, {
  wsUrl: SUBGRAPH_WS_URL,
  name: "indexer",
});

export const EXPLORER_URL = "https://paxscan.io";

// ── paxeer-network Symbol Mapping ──────────────────────────────────────────
export function toPaxeerNetworkSymbol(paxeerSymbol: string): string {
  return `PERP_${paxeerSymbol.toUpperCase()}_USDC`;
}

export function fromPaxeerNetworkSymbol(paxeerNetworkSymbol: string): string {
  return paxeerNetworkSymbol.replace("PERP_", "").replace("_USDC", "");
}

// ── Collateral tokens ────────────────────────────────────────────────
export const COLLATERAL_TOKENS = {
  usdc: {
    address: "0x4b29871681c95DFB2c7824BC4b0326B80217bCe8" as `0x${string}`,
    decimals: 6,
    symbol: "USDC",
  },
  usdt: {
    address: "0xe76f24bcF307290e4e09Ee45021CeC998c3749ce" as `0x${string}`,
    decimals: 6,
    symbol: "USDT",
  },
  usdl: {
    address: "0x85FcD13735F4309833A503EE804ea32395851479" as `0x${string}`,
    decimals: 6,
    symbol: "USDL",
  },
} as const;

export const COLLATERAL_OPTIONS = Object.entries(COLLATERAL_TOKENS).map(
  ([, val]) => ({
    label: val.symbol,
    value: val.address,
  })
);

// ── Spot tokens (from deployed.json spotTokens) ──────────────────────
// Source of truth: /deployed.json — v4.1 spot oracle-share deployment.
// Addresses are immutable; adding new tokens requires config update.
export interface SpotTokenMeta {
  address: `0x${string}`;
  decimals: number;
  symbol: string;
}

export const SPOT_TOKENS: Record<string, SpotTokenMeta> = {
  "0xf8850b62ae017c55be7f571bbad840b4f3da7d49": { address: "0xf8850b62AE017c55be7f571BBad840b4f3DA7D49", decimals: 6,  symbol: "USDC" },
  "0x5dfe06ae465a39c442c45ed273c523bac2d1f6a8": { address: "0x5dfE06Ae465a39c442c45ed273c523BaC2d1f6a8", decimals: 6,  symbol: "USDT" },
  "0x808cd264d7b43b2cda398b0c373d964a884c5c29": { address: "0x808cd264d7B43b2CdA398B0c373d964a884c5c29", decimals: 8,  symbol: "WBTC" },
  "0x5ba2f89f60f5805512a265bdfbb8984c85b4c9b7": { address: "0x5ba2f89F60f5805512A265bdFbB8984C85b4c9B7", decimals: 18, symbol: "WETH" },
  "0x38416f047c53c6d295aff15e2fd296b6c896e2d8": { address: "0x38416f047c53C6D295AfF15e2fD296B6C896E2d8", decimals: 9,  symbol: "WSOL" },
  "0x2ce6495af2f6cf20ea1b4d637dc2e882a0276f36": { address: "0x2cE6495AF2F6cF20ea1b4d637dC2E882a0276F36", decimals: 18, symbol: "WBNB" },
  "0x9dd7a0b03a827b295d0d5d9224f30c959c5e9a21": { address: "0x9dd7a0b03A827b295D0D5D9224f30C959c5E9A21", decimals: 18, symbol: "WAVAX" },
  "0x9bf12c9e779647cf758667a19fbba1af93e69816": { address: "0x9bF12C9e779647Cf758667a19fBBa1aF93E69816", decimals: 18, symbol: "WLINK" },
  "0x3fca5c78041bbdb86b6a12f5f9032573641262b4": { address: "0x3fca5c78041bbdb86B6a12f5F9032573641262B4", decimals: 6,  symbol: "WXRP" },
  "0x5c1c9a47d96b5ac04658ac8ba7004419219a7fae": { address: "0x5C1C9a47d96B5aC04658Ac8BA7004419219a7faE", decimals: 18, symbol: "WHYPE" },
  "0x86949e4cdb89496490890b67c9cff63ed8efb4b1": { address: "0x86949e4CdB89496490890B67C9cfF63eD8efB4b1", decimals: 6,  symbol: "SID" },
  "0x03baeba0f5fb591f235f6fe557ee002aa186af01": { address: "0x03bAeba0f5fB591F235f6fE557eE002Aa186Af01", decimals: 9,  symbol: "WSUI" },
  "0x7bf5ffa044dc7789bb3d71ea91d84a918a53a1f5": { address: "0x7Bf5FFa044DC7789Bb3d71EA91d84a918a53a1F5", decimals: 8,  symbol: "WDOGE" },
  "0xf2a3426dca121ec888992a91bbc3b9dbe9fc2005": { address: "0xF2a3426DCA121EC888992A91BBC3b9Dbe9fC2005", decimals: 8,  symbol: "WAPT" },
  "0xf28d269d89286444f77a65475c02f434671ea15e": { address: "0xF28D269D89286444F77a65475c02f434671ea15E", decimals: 6,  symbol: "WATOM" },
  "0x22272d9dd92cc7cf4cd19da225fa052642542c07": { address: "0x22272D9Dd92cc7cF4cD19dA225FA052642542C07", decimals: 8,  symbol: "WLTC" },
  "0xe19c59bc982979d510528dc867d34b7f4d81fd93": { address: "0xe19C59BC982979D510528Dc867D34B7F4D81Fd93", decimals: 10, symbol: "WDOT" },
  "0x36c918c4c0608132d2d1391d0bdfdcde4c2e88bd": { address: "0x36C918c4C0608132d2D1391d0BdFdCDE4c2e88BD", decimals: 9,  symbol: "WTON" },
};

// ── Spot market metadata (from deployed.json spotMarkets) ────────────
// Maps a spot marketId (bytes32 hex) to its display metadata and the
// perps marketId it shares an oracle with (v4.1 spot-oracle-share upgrade).
// Keys are lowercased for case-insensitive lookup.
export interface SpotMarketMeta {
  marketId: `0x${string}`;
  pair: string;             // e.g. "WBTC/USDC"
  baseSymbol: string;
  quoteSymbol: string;
  perpsMarketId: number;    // perps market whose oracle backs this spot market
}

export const SPOT_MARKETS_META: Record<string, SpotMarketMeta> = {
  "0xea9cd065291b0fee6f5c6c8589778a997db3463f32ac371c6f23788a9adb8682": { marketId: "0xea9cd065291b0fee6f5c6c8589778a997db3463f32ac371c6f23788a9adb8682", pair: "WBTC/USDC",  baseSymbol: "WBTC",  quoteSymbol: "USDC", perpsMarketId: 0 },
  "0x8e074e28da74455b1fa5c600acdb399d451811948d290b9304aa27ff659ee5bd": { marketId: "0x8e074e28da74455b1fa5c600acdb399d451811948d290b9304aa27ff659ee5bd", pair: "WETH/USDC",  baseSymbol: "WETH",  quoteSymbol: "USDC", perpsMarketId: 1 },
  "0x01f8c62487bc1c0ea8e8230e084559b51cbc9879610ba11bb2512d1af4e0446b": { marketId: "0x01f8c62487bc1c0ea8e8230e084559b51cbc9879610ba11bb2512d1af4e0446b", pair: "WSOL/USDC",  baseSymbol: "WSOL",  quoteSymbol: "USDC", perpsMarketId: 2 },
  "0x0e3e654fd6f07985688919c3be4a8e638029df39760663d9757f470c17036e31": { marketId: "0x0e3e654fd6f07985688919c3be4a8e638029df39760663d9757f470c17036e31", pair: "WAVAX/USDC", baseSymbol: "WAVAX", quoteSymbol: "USDC", perpsMarketId: 3 },
  "0xa8a732877f15428cb1143e19f2e1aff46ef2a4677b894f887fcf6d9451e1e4e6": { marketId: "0xa8a732877f15428cb1143e19f2e1aff46ef2a4677b894f887fcf6d9451e1e4e6", pair: "WLINK/USDC", baseSymbol: "WLINK", quoteSymbol: "USDC", perpsMarketId: 4 },
  "0x8db3c7ee349da5ab71780b338cfbb4fceb63b52a09c39f0ebfa298b5333a9880": { marketId: "0x8db3c7ee349da5ab71780b338cfbb4fceb63b52a09c39f0ebfa298b5333a9880", pair: "WXRP/USDC",  baseSymbol: "WXRP",  quoteSymbol: "USDC", perpsMarketId: 14 },
  "0x7926c8bd23673fbba4fbed335c389b6bc675e79fc7c0fe12cbebd24edaebc9f1": { marketId: "0x7926c8bd23673fbba4fbed335c389b6bc675e79fc7c0fe12cbebd24edaebc9f1", pair: "WHYPE/USDC", baseSymbol: "WHYPE", quoteSymbol: "USDC", perpsMarketId: 13 },
  "0x15f8d8830d211da52e4ed7efdabb992a681e32f950f02934476d9ef7b58aece7": { marketId: "0x15f8d8830d211da52e4ed7efdabb992a681e32f950f02934476d9ef7b58aece7", pair: "WBNB/USDC",  baseSymbol: "WBNB",  quoteSymbol: "USDC", perpsMarketId: 17 },
  "0x399a9122923c78f10df055f38baa36c9d4020bb00546abcd5c9be452d6d2d343": { marketId: "0x399a9122923c78f10df055f38baa36c9d4020bb00546abcd5c9be452d6d2d343", pair: "SID/USDC",   baseSymbol: "SID",   quoteSymbol: "USDC", perpsMarketId: 12 },
};

/** Look up the perps market a spot market shares an oracle with. */
export function getPerpsMarketIdForSpot(
  spotMarketId: string
): number | undefined {
  return SPOT_MARKETS_META[spotMarketId.toLowerCase()]?.perpsMarketId;
}

/** Look up spot market display metadata by marketId. */
export function getSpotMarketMeta(
  spotMarketId: string
): SpotMarketMeta | undefined {
  return SPOT_MARKETS_META[spotMarketId.toLowerCase()];
}

// ── Markets ──────────────────────────────────────────────────────────
export interface PerpsMarket {
  marketId: number;
  symbol: string;
  name: string;
  baseToken: string;
  maxLeverage: number;
  description?: string;
}

export const PERPS_MARKETS: PerpsMarket[] = [
  { marketId: 0, symbol: "BTC", name: "Bitcoin", baseToken: "BTC", maxLeverage: 1000, description: "Bitcoin is the original cryptocurrency and the largest digital asset by market cap. Created in 2009, it operates on a decentralized network and is widely considered a store of value, often called digital gold." },
  { marketId: 1, symbol: "ETH", name: "Ethereum", baseToken: "ETH", maxLeverage: 1000, description: "Ethereum is the leading smart contract platform powering thousands of decentralized applications, DeFi protocols, and NFTs. Its native token ETH is used to pay for transactions across the network." },
  { marketId: 2, symbol: "SOL", name: "Solana", baseToken: "SOL", maxLeverage: 500, description: "Solana is a high-performance blockchain known for fast transaction speeds and low fees. It has become a popular platform for DeFi, NFTs, and consumer-facing crypto apps." },
  { marketId: 3, symbol: "AVAX", name: "Avalanche", baseToken: "AVAX", maxLeverage: 500, description: "Avalanche is a fast, low-cost smart contract platform designed for DeFi and enterprise applications. It uses a novel consensus protocol that can process thousands of transactions per second." },
  { marketId: 4, symbol: "LINK", name: "Chainlink", baseToken: "LINK", maxLeverage: 200, description: "Chainlink is the leading decentralized oracle network, connecting smart contracts to real-world data like prices, weather, and events. It powers most of DeFi's price feeds." },
  { marketId: 5, symbol: "TSLA", name: "Tesla", baseToken: "TSLA", maxLeverage: 500, description: "Tesla is an electric vehicle and clean energy company led by Elon Musk. It designs and manufactures EVs, battery storage, and solar panels. TSLA is one of the most actively traded stocks globally." },
  { marketId: 6, symbol: "NVDA", name: "NVIDIA", baseToken: "NVDA", maxLeverage: 500, description: "NVIDIA designs graphics processing units (GPUs) used in gaming, data centers, and AI training. It is a dominant player in the AI hardware market and one of the most valuable companies in the world." },
  { marketId: 7, symbol: "NAS100", name: "Nasdaq 100", baseToken: "NAS100", maxLeverage: 500, description: "The Nasdaq 100 tracks the 100 largest non-financial companies listed on the Nasdaq exchange, heavily weighted toward technology giants like Apple, Microsoft, and NVIDIA." },
  { marketId: 8, symbol: "XAU", name: "Gold", baseToken: "XAU", maxLeverage: 500, description: "Gold is the world's oldest store of value and a traditional safe-haven asset. Investors turn to gold during economic uncertainty. It is priced per troy ounce in US dollars." },
  { marketId: 9, symbol: "SPX500", name: "S&P 500", baseToken: "SPX500", maxLeverage: 500, description: "The S&P 500 is an index of the 500 largest publicly traded companies in the United States. It is widely regarded as the best single gauge of the US stock market and economy." },
  { marketId: 10, symbol: "GOOGL", name: "Alphabet", baseToken: "GOOGL", maxLeverage: 500, description: "Alphabet is the parent company of Google, the world's largest search engine and advertising platform. It also operates YouTube, Google Cloud, Waymo, and DeepMind." },
  { marketId: 11, symbol: "PAX", name: "Paxeer Network", baseToken: "PAX", maxLeverage: 1000, description: "Paxeer Network is a next-generation Layer 1 blockchain designed for high-throughput trading applications. It powers this exchange and provides near-instant transaction finality." },
  { marketId: 12, symbol: "SID", name: "Sidiora Markets", baseToken: "SID", maxLeverage: 1000, description: "Sidiora Markets is the decentralized exchange protocol built on Paxeer Network. SID is the governance and utility token that accrues fees from all trading activity on the platform." },
  { marketId: 13, symbol: "HYPE", name: "Hyperliquid", baseToken: "HYPE", maxLeverage: 500, description: "Hyperliquid is a high-performance decentralized perpetual exchange running on its own L1 chain. It offers an order-book trading experience with deep liquidity and fast execution." },
  { marketId: 14, symbol: "XRP", name: "XRP", baseToken: "XRP", maxLeverage: 500, description: "XRP is a digital payment token created by Ripple Labs, designed for fast and cheap cross-border money transfers. It is one of the oldest and most well-known cryptocurrencies." },
  { marketId: 15, symbol: "ASTER", name: "Aster", baseToken: "ASTER", maxLeverage: 500, description: "Aster is an emerging blockchain project focused on modular infrastructure and developer tooling. It aims to simplify building decentralized applications with composable building blocks." },
  { marketId: 16, symbol: "TRUMP", name: "Trump", baseToken: "TRUMP", maxLeverage: 500, description: "TRUMP is a meme token associated with political and cultural events. It is a high-volatility speculative asset popular among retail traders looking for short-term momentum plays." },
  { marketId: 17, symbol: "BNB", name: "Binance Smart Chain", baseToken: "BNB", maxLeverage: 500, description: "BNB is the native token of the BNB Chain ecosystem, originally created as the utility token for the Binance exchange. It is used for transaction fees, staking, and governance." },
];

// ── Market Categories (V2) ──────────────────────────────────────────
export const MARKET_CATEGORIES: Record<string, number[]> = {
  crypto: [0, 1, 2, 3, 4, 11, 12, 13, 14, 15, 16, 17],
  stocks: [5, 6, 10],
  indices: [7, 9],
  commodities: [8],
};

export const MARKET_CATEGORY_LABELS: Record<string, string> = {
  all: "All Markets",
  crypto: "Crypto",
  stocks: "Stocks",
  indices: "Indices",
  commodities: "Commodities",
};

export function getMarketCategory(marketId: number): string {
  for (const [cat, ids] of Object.entries(MARKET_CATEGORIES)) {
    if (ids.includes(marketId)) return cat;
  }
  return "crypto";
}

// ── Order Types (V2) ────────────────────────────────────────────────
export const ORDER_TYPE_LABELS: Record<number, string> = {
  0: "Limit",
  1: "Stop-Limit",
  2: "Take Profit",
  3: "Stop Loss",
};

export const ORDER_TYPE_COLORS: Record<number, string> = {
  0: "text-blue-400",
  1: "text-amber-400",
  2: "text-emerald-500",
  3: "text-red-500",
};

export function marketLogoUrl(symbol: string): string {
  return coinLogoUrl(symbol);
}

export function coinLogoUrl(symbol: string): string {
  return `https://raw.githubusercontent.com/Paxeer-Network/Pax-OrbexOpenSourceExchange/refs/heads/main/public/img/crypto/${symbol.toLowerCase()}.webp`;
}

// ── ERC20 ABI (minimal for approve/allowance/balanceOf) ──────────────
export const erc20Abi = [
  { type: "function", name: "approve", inputs: [{ name: "spender", type: "address" }, { name: "amount", type: "uint256" }], outputs: [{ type: "bool" }], stateMutability: "nonpayable" },
  { type: "function", name: "allowance", inputs: [{ name: "owner", type: "address" }, { name: "spender", type: "address" }], outputs: [{ type: "uint256" }], stateMutability: "view" },
  { type: "function", name: "balanceOf", inputs: [{ name: "account", type: "address" }], outputs: [{ type: "uint256" }], stateMutability: "view" },
] as const;

// === DATA AGENT ADDITIONS ===

// ── Spot Order Side Labels ─────────────────────────────────────────────
export const SPOT_SIDE_LABELS: Record<number, string> = {
  0: "Buy",
  1: "Sell",
};

// ── Spot Order Type Labels ─────────────────────────────────────────────
export const SPOT_ORDER_TYPE_LABELS: Record<number, string> = {
  0: "Market",
  1: "Limit",
};

// ── Spot Trading Mode Labels ───────────────────────────────────────────
export const SPOT_MODE_LABELS: Record<number, string> = {
  0: "Continuous",
  1: "Batch",
};

// ── Fee Type Labels (V5) ───────────────────────────────────────────────
export const FEE_TYPE_LABELS: Record<number, string> = {
  0: "Trading",
  1: "Borrowing",
  2: "Funding",
  3: "Liquidation",
};

// ── Trade Type Labels (V5 TradeSettlement) ─────────────────────────────
export const TRADE_TYPE_LABELS: Record<number, string> = {
  0: "Open",
  1: "Close",
  2: "Liquidation",
  3: "PartialClose",
};

// ── PoFQ Tier Labels ──────────────────────────────────────────────────
export const POFQ_TIER_LABELS: Record<number, string> = {
  0: "Bronze",
  1: "Silver",
  2: "Gold",
  3: "Platinum",
};

// ── Margin Mode Labels ────────────────────────────────────────────────
export const MARGIN_MODE_LABELS: Record<number, string> = {
  0: "Cross",
  1: "Isolated",
};

// ── Vault Type Labels (V5 VaultBalanceRecord) ──────────────────────────
export const VAULT_TYPE_LABELS: Record<number, string> = {
  0: "Central",
  1: "Insurance",
  2: "Fee",
};

// ── Analytics (Track B) ───────────────────────────────────────────────
export const TRACK_B_STRINGS = {
  ANALYTICS_TITLE: "Analytics",
  ANALYTICS_SUBTITLE: "Protocol-wide and per-market analytics",
  FEES_CARD_TITLE: "Fees Breakdown",
  FEES_EMPTY: "No fees recorded yet",
  SETTLEMENTS_EMPTY: "No settlements recorded yet",
  FUNDING_CHART_TITLE: "Funding Rate (24h)",
  MARK_PRICE_CHART_TITLE: "Mark Price vs Index Price",
  TVL_CHART_TITLE: "Protocol TVL",
  OI_CHART_TITLE: "Open Interest",
  MARKET_SNAPSHOT_TABLE_TITLE: "Market Snapshots",
} as const;

export const ANALYTICS_CHART_COLORS = {
  // Migrated 2026-04-23 (Phase 2 analytics) to brand palette.
  // `tvl` was violet-500 — banned by AGENTS.md §20 (no purple defaults). Now brand-blue-200.
  markPrice: "#0ebc5f",              // brand green — matches --color-long
  indexPrice: "hsl(217, 91%, 60%)",  // blue-500 — kept for mark/index contrast
  funding: "#fa7b17",                // brand warning orange — matches --brand-orange intent
  oi: "#0ebc5f",                     // brand green — matches --color-long
  oiShort: "#ff4c45",                // brand red — matches --color-short
  tvl: "#8FA8FF",                    // brand-blue-200 (replaces violet-500 — charter §20)
} as const;

export const ANALYTICS_AXIS_FORMATTERS = {
  shortDate: (iso: string | number): string => {
    const d = typeof iso === "number" ? new Date(iso) : new Date(iso);
    if (isNaN(d.getTime())) return String(iso);
    return d.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  },
  percent: (v: number): string => {
    if (!Number.isFinite(v)) return "—";
    return `${(v * 100).toFixed(2)}%`;
  },
  compactUsd: (v: number): string => {
    if (!Number.isFinite(v) || v === 0) return "$0";
    const abs = Math.abs(v);
    const sign = v < 0 ? "-" : "";
    if (abs >= 1_000_000_000) return `${sign}$${(abs / 1_000_000_000).toFixed(2)}B`;
    if (abs >= 1_000_000) return `${sign}$${(abs / 1_000_000).toFixed(2)}M`;
    if (abs >= 1_000) return `${sign}$${(abs / 1_000).toFixed(2)}K`;
    return `${sign}$${abs.toFixed(2)}`;
  },
} as const;

// ── PAX Home featured markets ─────────────────────────────────────────
export const FEATURED_MARKET_SYMBOLS: readonly string[] = [
  "BTC", "ETH", "SOL", "PAX", "SID", "XRP",
] as const;

// ── Spot mode banner copy ─────────────────────────────────────────────
export const SPOT_MODE_BANNER_COPY =
  "Batch mode active — orders settle at the next epoch boundary.";

// ── Default slippage (bps) ────────────────────────────────────────────
export const DEFAULT_SLIPPAGE_BPS = 50; // 0.5%

// ── Position action modal copy ────────────────────────────────────────
export const POSITION_ACTION_COPY = {
  addCollateral: "Add Collateral",
  removeCollateral: "Remove Collateral",
  addSize: "Add Size",
  topUp: "Top Up Position",
  transferMargin: "Transfer Margin",
  amountLabel: "Amount",
  additionalCollateralLabel: "Additional Collateral",
  leverageLabel: "Leverage",
  remainingMarginLabel: "Remaining Margin Ratio",
  toPositionLabel: "To Position ID",
  successRemoveCollateral: "Collateral removed",
  successAddSize: "Size added",
  successTopUp: "Position topped up",
  successTransferMargin: "Margin transferred",
  errorInsufficientMargin: "Remaining collateral too low for maintenance",
  errorAddSize: "Failed to add size",
  errorRemoveCollateral: "Failed to remove collateral",
  errorTopUp: "Top up failed",
  errorTransferMargin: "Margin transfer failed",
} as const;

// ── Margin mode toggle copy ───────────────────────────────────────────
export const MARGIN_MODE_TOGGLE_COPY = {
  cross: "Cross",
  isolated: "Isolated",
  successSwitch: "Switched to %MODE% margin",
  errorSwitch: "Failed to switch margin mode",
  confirmTitle: "Switch Margin Mode",
  confirmSwitch: "Switch this position to %MODE% margin?",
  confirmAction: "Confirm",
} as const;

// ── TP / SL modal copy ────────────────────────────────────────────────
export const TP_SL_COPY = {
  modalTitle: "Set Take Profit / Stop Loss",
  tpLabel: "Take Profit Price",
  slLabel: "Stop Loss Price",
  tpPlaceholder: "Target price",
  slPlaceholder: "Stop price",
  submitLabel: "Submit",
  successTp: "Take Profit order placed",
  successSl: "Stop Loss order placed",
  errorTp: "Take Profit order failed",
  errorSl: "Stop Loss order failed",
  validationAtLeastOne: "Enter at least one of TP or SL",
  validationTpLong: "TP must be above mark price for long",
  validationTpShort: "TP must be below mark price for short",
  validationSlLong: "SL must be below mark price for long",
  validationSlShort: "SL must be above mark price for short",
} as const;
