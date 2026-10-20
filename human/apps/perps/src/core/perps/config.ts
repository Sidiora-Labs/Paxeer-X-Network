import { paxeerChain } from "@/core/chain";
import { gqlEndpoint, type GraphQLEndpoint } from "@/core/graphql/client";
import { gatewayUrls } from "@/core/network/gateway";

// ── Chain (re-export from canonical core location for back-compat) ───
// The chain object lives at @/core/chain. Re-exported here so existing
// `import { paxeerChain } from "./config"` consumers keep working until
// they're individually migrated to import from @/core/chain.
export { paxeerChain } from "@/core/chain";

/**
 * Indexer GraphQL endpoint on the gateway: POST queries on /graphql and
 * graphql-transport-ws subscriptions on the ws form of the same origin.
 */
export const INDEXER_GRAPHQL: GraphQLEndpoint = gqlEndpoint(gatewayUrls().graphql.url, {
  wsUrl: gatewayUrls().graphql.wsUrl,
  name: "indexer",
});

export const EXPLORER_URL = paxeerChain.blockExplorers.default.url;

// ── Orderly Symbol Mapping ──────────────────────────────────────────
export function toOrderlySymbol(paxeerSymbol: string): string {
  return `PERP_${paxeerSymbol.toUpperCase()}_USDC`;
}

export function fromOrderlySymbol(orderlySymbol: string): string {
  return orderlySymbol.replace("PERP_", "").replace("_USDC", "");
}

// ── Collateral tokens ────────────────────────────────────────────────
export const COLLATERAL_TOKENS = {
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
  { marketId: 2, symbol: "SOL", name: "Solana", baseToken: "SOL", maxLeverage: 1000, description: "Solana is a high-performance blockchain known for fast transaction speeds and low fees. It has become a popular platform for DeFi, NFTs, and consumer-facing crypto apps." },
  { marketId: 3, symbol: "AVAX", name: "Avalanche", baseToken: "AVAX", maxLeverage: 1000, description: "Avalanche is a fast, low-cost smart contract platform designed for DeFi and enterprise applications. It uses a novel consensus protocol that can process thousands of transactions per second." },
  { marketId: 4, symbol: "LINK", name: "Chainlink", baseToken: "LINK", maxLeverage: 1000, description: "Chainlink is the leading decentralized oracle network, connecting smart contracts to real-world data like prices, weather, and events. It powers most of DeFi's price feeds." },
  { marketId: 5, symbol: "TSLA", name: "Tesla", baseToken: "TSLA", maxLeverage: 1000, description: "Tesla is an electric vehicle and clean energy company led by Elon Musk. It designs and manufactures EVs, battery storage, and solar panels. TSLA is one of the most actively traded stocks globally." },
  { marketId: 6, symbol: "NVDA", name: "NVIDIA", baseToken: "NVDA", maxLeverage: 1000, description: "NVIDIA designs graphics processing units (GPUs) used in gaming, data centers, and AI training. It is a dominant player in the AI hardware market and one of the most valuable companies in the world." },
  { marketId: 7, symbol: "NAS100", name: "Nasdaq 100", baseToken: "NAS100", maxLeverage: 1000, description: "The Nasdaq 100 tracks the 100 largest non-financial companies listed on the Nasdaq exchange, heavily weighted toward technology giants like Apple, Microsoft, and NVIDIA." },
  { marketId: 8, symbol: "XAU", name: "Gold", baseToken: "XAU", maxLeverage: 1000, description: "Gold is the world's oldest store of value and a traditional safe-haven asset. Investors turn to gold during economic uncertainty. It is priced per troy ounce in US dollars." },
  { marketId: 9, symbol: "SPX500", name: "S&P 500", baseToken: "SPX500", maxLeverage: 1000, description: "The S&P 500 is an index of the 500 largest publicly traded companies in the United States. It is widely regarded as the best single gauge of the US stock market and economy." },
  { marketId: 10, symbol: "GOOGL", name: "Alphabet", baseToken: "GOOGL", maxLeverage: 1000, description: "Alphabet is the parent company of Google, the world's largest search engine and advertising platform. It also operates YouTube, Google Cloud, Waymo, and DeepMind." },
  { marketId: 11, symbol: "PAX", name: "Paxeer Network", baseToken: "PAX", maxLeverage: 1000, description: "Paxeer Network is a next-generation Layer 1 blockchain designed for high-throughput trading applications. It powers this exchange and provides near-instant transaction finality." },
  { marketId: 12, symbol: "SID", name: "Sidiora Markets", baseToken: "SID", maxLeverage: 1000, description: "Sidiora Markets is the decentralized exchange protocol built on Paxeer Network. SID is the governance and utility token that accrues fees from all trading activity on the platform." },
  { marketId: 13, symbol: "HYPE", name: "Hyperliquid", baseToken: "HYPE", maxLeverage: 1000, description: "Hyperliquid is a high-performance decentralized perpetual exchange running on its own L1 chain. It offers an order-book trading experience with deep liquidity and fast execution." },
  { marketId: 14, symbol: "XRP", name: "XRP", baseToken: "XRP", maxLeverage: 1000, description: "XRP is a digital payment token created by Ripple Labs, designed for fast and cheap cross-border money transfers. It is one of the oldest and most well-known cryptocurrencies." },
  { marketId: 15, symbol: "ASTER", name: "Aster", baseToken: "ASTER", maxLeverage: 1000, description: "Aster is an emerging blockchain project focused on modular infrastructure and developer tooling. It aims to simplify building decentralized applications with composable building blocks." },
  { marketId: 16, symbol: "TRUMP", name: "Trump", baseToken: "TRUMP", maxLeverage: 1000, description: "TRUMP is a meme token associated with political and cultural events. It is a high-volatility speculative asset popular among retail traders looking for short-term momentum plays." },
  { marketId: 17, symbol: "BNB", name: "Binance Smart Chain", baseToken: "BNB", maxLeverage: 1000, description: "BNB is the native token of the BNB Chain ecosystem, originally created as the utility token for the Binance exchange. It is used for transaction fees, staking, and governance." },
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
  4: "Market",
  5: "Stop Market",
};

export const ORDER_TYPE_COLORS: Record<number, string> = {
  0: "text-blue-400",
  1: "text-amber-400",
  2: "text-emerald-500",
  3: "text-red-500",
  4: "text-zinc-300",
  5: "text-orange-400",
};

export function marketLogoUrl(symbol: string): string {
  return coinLogoUrl(symbol);
}

// Logos served from public/market-logos/<symbol>.<ext>, keyed by lowercase symbol.
export const MARKET_LOGO_FILES: Readonly<Record<string, string>> = Object.freeze({
  btc: "btc.png", eth: "eth.png", sol: "sol.png", avax: "avax.png", link: "link.png",
  tsla: "tsla.webp", nvda: "nvda.webp", nas100: "nas100.webp", xau: "xau.jpg", spx500: "spx500.webp",
  googl: "googl.webp", pax: "pax.png", sid: "sid.png", hype: "hype.jpg", xrp: "xrp.png",
  aster: "aster.png", trump: "trump.png", bnb: "bnb.png", usdl: "usdl.jpg",
});

export function coinLogoUrl(symbol: string): string {
  const key = symbol.toLowerCase();
  return `/market-logos/${MARKET_LOGO_FILES[key] ?? `${key}.png`}`;
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
  4: "Maker Rebate",
};

// ── Trade Type Labels (V5 TradeSettlement) ─────────────────────────────
export const TRADE_TYPE_LABELS: Record<number, string> = {
  0: "Open",
  1: "Close",
  2: "Liquidation",
  3: "PartialClose",
  4: "Increase",
  5: "ADL",
  6: "Backstop",
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
  amountLabel: "Amount",
  additionalCollateralLabel: "Additional Collateral",
  leverageLabel: "Leverage",
  remainingMarginLabel: "Remaining Margin Ratio",
  toPositionLabel: "To Position ID",
  successRemoveCollateral: "Collateral removed",
  successAddSize: "Size added",
  successTopUp: "Position topped up",
  errorInsufficientMargin: "Remaining collateral too low for maintenance",
  errorAddSize: "Failed to add size",
  errorRemoveCollateral: "Failed to remove collateral",
  errorTopUp: "Top up failed",
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
