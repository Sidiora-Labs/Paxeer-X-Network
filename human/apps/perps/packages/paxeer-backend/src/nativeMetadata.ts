import {
  PERPS_MARKETS,
  COLLATERAL_TOKENS,
  getMarketCategory,
} from "./core/perps/config";
import { paxeerChain } from "./core/chain";
import { getNativeSnapshot } from "./runtime";
export const toNativeSymbol = (symbol: string) =>
  symbol
    .replace(/^PERP_/, "")
    .replace(/_USDC$/, "")
    .toUpperCase();
export const toTradingSymbol = (symbol: string) =>
  `PERP_${toNativeSymbol(symbol)}_USDC`;
export function nativeSymbols() {
  // Paxeer positions are sized in USD collateral. These ticks define the
  // native UI's base-quantity and quote-input precision, not a remote order lot.
  return PERPS_MARKETS.map((market) => ({
    marketId: market.marketId,
    symbol: toTradingSymbol(market.symbol),
    display_symbol_name: market.symbol,
    base_tick: 0.00000001,
    base_min: 0.00000001,
    base_max: Number.MAX_SAFE_INTEGER,
    quote_tick: 0.000001,
    quote_min: 0.000001,
    quote_max: Number.MAX_SAFE_INTEGER,
    min_notional: 0.000001,
    base_imr: 1 / market.maxLeverage,
    base_mmr: undefined,
    imr_factor: undefined,
    interest_rate: undefined,
    cap_funding: undefined,
    floor_funding: undefined,
    liquidation_fee: undefined,
    max_leverage: market.maxLeverage,
    funding_period: 8,
    category: getMarketCategory(market.marketId),
    is_rwa: ["stocks", "indices", "commodities"].includes(
      getMarketCategory(market.marketId),
    ),
  }));
}
export function nativeMarketRows() {
  const snapshots = new Map(
    getNativeSnapshot().markets.map((row) => [row.symbol.toUpperCase(), row]),
  );
  return nativeSymbols().map((info) => {
    const row = snapshots.get(toNativeSymbol(info.symbol));
    const price = row?.price ?? undefined;
    const open =
      row && Number.isFinite(row.change_24h)
        ? price / (1 + row.change_24h / 100)
        : undefined;
    return {
      ...info,
      mark_price: price,
      index_price: row?.index_price ?? price,
      last_price: price,
      "24h_open": open,
      "24h_close": price,
      "24h_high": row?.high_24h ?? undefined,
      "24h_low": row?.low_24h ?? undefined,
      "24h_volumn": row?.amount_24h ?? undefined,
      "24h_volume": row?.amount_24h ?? undefined,
      "24h_amount": row?.volume_24h ?? undefined,
      open_interest:
        row?.open_interest !== undefined && price
          ? row.open_interest / price
          : undefined,
      est_funding_rate: row?.est_funding_rate ?? undefined,
      last_funding_rate: row?.funding_8h ?? undefined,
      next_funding_time: row?.next_funding_time,
      timestamp: row?.timestamp,
    };
  });
}
export function nativeChains() {
  return [
    {
      chain_id: paxeerChain.id,
      name: paxeerChain.name,
      network_infos: {
        chain_id: paxeerChain.id,
        name: paxeerChain.name,
        shortName: "Paxeer",
        public_rpc_url: paxeerChain.rpcUrls.default.http[0],
        block_explorer_url: "https://paxscan.io",
        bridgeless: true,
        chain_namespace: "EVM",
        currency_symbol: "PAX",
        currency_decimal: 18,
      },
      token_infos: Object.values(COLLATERAL_TOKENS).map((token) => ({
        ...token,
        chain_id: paxeerChain.id,
        is_native: false,
        is_collateral: true,
        token_hash: token.address,
      })),
    },
  ];
}
export function nativeTokens() {
  return Object.values(COLLATERAL_TOKENS).map((token) => ({
    token: token.symbol,
    symbol: token.symbol,
    decimals: token.decimals,
    token_hash: token.address,
    is_collateral: true,
    collateral_ratio: 1,
    discount_factor: 1,
    minimum_withdraw_amount: 0.000001,
    chain_details: [
      {
        ...token,
        chain_id: paxeerChain.id,
        deposit_fee: undefined,
        withdraw_fee: undefined,
        allow_deposit: true,
        allow_withdraw: true,
      },
    ],
  }));
}
