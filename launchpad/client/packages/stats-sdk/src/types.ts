// Wire contract: core/stats-workers/internal/store/store.go PoolStatsRow.
export interface PoolStats {
  poolAddress: string;
  tokenAddress: string;
  price: string;
  priceChange1m: string;
  priceChange5m: string;
  priceChange15m: string;
  priceChange1h: string;
  priceChange24h: string;
  priceChangeDollar1m: string;
  priceChangeDollar5m: string;
  priceChangeDollar15m: string;
  priceChangeDollar1h: string;
  priceChangeDollar24h: string;
  high24h: string;
  low24h: string;
  volume24h: string;
  volume1h: string;
  volume5m: string;
  marketCap: string;
  buyCount24h: number;
  sellCount24h: number;
  uniqueTraders24h: number;
  holderCount: number;
  top10Concentration: string;
  creatorHoldingsPct: string;
  riskRating: number;
  riskFactors: string | null;
  creatorAddress: string | null;
  createdAt: number;
  updatedAt: number;
}

export const poolStatsFields = {
  "poolAddress": "string",
  "tokenAddress": "string",
  "price": "string",
  "priceChange1m": "string",
  "priceChange5m": "string",
  "priceChange15m": "string",
  "priceChange1h": "string",
  "priceChange24h": "string",
  "priceChangeDollar1m": "string",
  "priceChangeDollar5m": "string",
  "priceChangeDollar15m": "string",
  "priceChangeDollar1h": "string",
  "priceChangeDollar24h": "string",
  "high24h": "string",
  "low24h": "string",
  "volume24h": "string",
  "volume1h": "string",
  "volume5m": "string",
  "marketCap": "string",
  "buyCount24h": "number",
  "sellCount24h": "number",
  "uniqueTraders24h": "number",
  "holderCount": "number",
  "top10Concentration": "string",
  "creatorHoldingsPct": "string",
  "riskRating": "number",
  "riskFactors": "string | null",
  "creatorAddress": "string | null",
  "createdAt": "number",
  "updatedAt": "number"
} as const;
