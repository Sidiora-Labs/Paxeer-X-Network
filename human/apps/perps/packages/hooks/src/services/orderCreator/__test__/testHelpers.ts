import {
  API,
  OrderEntity,
  PaxeerNetworkOrder,
  OrderSide,
} from "@paxeer-network/types";
import { ValuesDepConfig } from "../interface";

/**
 * Creates a mock symbol configuration for testing
 * @param overrides - Optional overrides for specific fields
 * @returns A complete symbol configuration object
 */
export const createMockSymbolConfig = (
  overrides?: Partial<API.SymbolExt>,
): API.SymbolExt => {
  return {
    symbol: "PERP_ETH_USDC",
    quote_min: 0,
    quote_max: 100000,
    quote_tick: 0.01,
    base_min: 0.0001,
    base_max: 320,
    base_tick: 0.0001,
    min_notional: 10,
    price_range: 0.03,
    price_scope: 0.4,
    std_liquidation_fee: 0.025,
    liquidator_fee: 0.0125,
    claim_insurance_fund_discount: 0.0075,
    funding_period: 8,
    cap_funding: 0.00375,
    floor_funding: -0.00375,
    interest_rate: 0.0001,
    created_time: 1693820915671,
    updated_time: 1709888598319,
    liquidation_tier: 1,
    base_mmr: 0.025,
    base_imr: 0.05,
    base_dp: 4,
    quote_dp: 2,
    base: "ETH",
    quote: "USDC",
    type: "PERP",
    name: "ETH-PERP",
    ...overrides,
  };
};

/**
 * Creates a mock ValuesDepConfig for testing
 * @param overrides - Optional overrides for specific fields
 * @returns A complete ValuesDepConfig object
 */
export const createMockConfig = (
  overrides?: Partial<ValuesDepConfig>,
): ValuesDepConfig => {
  return {
    symbol: createMockSymbolConfig(),
    maxQty: 3.5,
    markPrice: 4000,
    estSlippage: null,
    askAndBid: undefined,
    ...overrides,
  };
};

/**
 * Creates a mock OrderEntity for testing
 * @param overrides - Optional overrides for specific fields
 * @returns A complete OrderEntity object
 */
export const createMockOrderEntity = (
  overrides?: Partial<OrderEntity>,
): OrderEntity => {
  return {
    reduce_only: false,
    side: OrderSide.BUY,
    order_type: "LIMIT",
    isStopOrder: false,
    symbol: "PERP_ETH_USDC",
    visible_quantity: 1,
    order_price: "4015.41",
    order_quantity: "0.2936",
    trigger_price: "",
    total: "1178.92",
    ...overrides,
  } as OrderEntity;
};

/**
 * Creates a mock PaxeerNetworkOrder for testing
 * @param overrides - Optional overrides for specific fields
 * @returns A complete PaxeerNetworkOrder object
 */
export const createMockPaxeerNetworkOrder = (
  overrides?: Partial<PaxeerNetworkOrder>,
): PaxeerNetworkOrder => {
  return {
    reduce_only: false,
    side: OrderSide.BUY,
    order_type: "LIMIT",
    symbol: "PERP_ETH_USDC",
    visible_quantity: 1,
    order_price: "4015.41",
    order_quantity: "0.2936",
    total: "1178.92",
    margin_mode: "CROSS",
    ...overrides,
  } as PaxeerNetworkOrder;
};
