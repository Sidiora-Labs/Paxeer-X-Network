import { parseUnits } from "viem";
import Decimal from "decimal.js";
import { PERPS_MARKETS, COLLATERAL_TOKENS } from "./core/perps/config";
import { toNativeSymbol } from "./nativeMetadata";
export type NativeOrder = {
  symbol: string;
  side: string;
  order_type?: string;
  type?: string;
  order_quantity?: string | number;
  quantity?: string | number;
  order_price?: string | number;
  trigger_price?: string | number;
  reduce_only?: boolean;
  leverage?: number;
  collateral_token?: string;
  native_position_id?: string | number;
  native_close_all?: boolean;
  native_quote_id?: string;
  order_type_ext?: string;
  visible_quantity?: string | number;
};
export function planNativeOrder(
  order: NativeOrder,
  markPrice: number,
  leverage: number,
) {
  const market = PERPS_MARKETS.find(
    (item) => item.symbol === toNativeSymbol(order.symbol),
  );
  if (!market) throw new Error("Market is not supported by paxeer-network.");
  const quantity = new Decimal(order.order_quantity ?? order.quantity ?? 0);
  const type = (order.order_type ?? order.type ?? "MARKET").toUpperCase();
  if (
    ![
      "MARKET",
      "LIMIT",
      "STOP_LIMIT",
      "STOP_LOSS_LIMIT",
      "TAKE_PROFIT_LIMIT",
    ].includes(type)
  )
    throw new Error(
      `Paxeer contracts do not support ${type} through this order path.`,
    );
  if (order.reduce_only && type !== "MARKET")
    throw new Error("Use native position TP/SL for a conditional close.");
  if (order.order_type_ext)
    throw new Error(
      `Paxeer does not support ${order.order_type_ext} execution instructions.`,
    );
  if (order.visible_quantity !== undefined)
    throw new Error("Native on-chain orders cannot be hidden.");
  if (
    order.collateral_token &&
    !["USDC", COLLATERAL_TOKENS.usdc.address.toUpperCase()].includes(
      order.collateral_token.toUpperCase(),
    )
  )
    throw new Error("This order entry uses USDC collateral.");
  const side = order.side.toUpperCase();
  if (!["BUY", "SELL"].includes(side)) throw new Error("Choose Buy or Sell.");
  if (!quantity.isFinite() || quantity.lte(0))
    throw new Error("Order quantity must be positive.");
  if (
    !Number.isFinite(leverage) ||
    leverage < 1 ||
    leverage > market.maxLeverage
  )
    throw new Error("Leverage exceeds this Paxeer market's limit.");
  const price = new Decimal(
    type === "MARKET" ? markPrice : (order.order_price ?? markPrice),
  );
  if (!price.isFinite() || price.lte(0))
    throw new Error("A current Paxeer quote is required before submitting.");
  const notional = quantity.mul(price);
  const collateral = notional
    .div(leverage)
    .toDecimalPlaces(COLLATERAL_TOKENS.usdc.decimals, Decimal.ROUND_DOWN);
  if (collateral.lte(0))
    throw new Error("Collateral is below the token's smallest unit.");
  const trigger = new Decimal(
    order.trigger_price || order.order_price || price,
  );
  if (!trigger.isFinite() || trigger.lte(0))
    throw new Error("Trigger price must be positive.");
  return {
    marketId: BigInt(market.marketId),
    symbol: market.symbol,
    isLong: side === "BUY",
    type,
    collateralToken: COLLATERAL_TOKENS.usdc.address,
    collateralAmount: parseUnits(
      collateral.toFixed(COLLATERAL_TOKENS.usdc.decimals),
      COLLATERAL_TOKENS.usdc.decimals,
    ),
    leverage: parseUnits(new Decimal(leverage).toFixed(18), 18),
    sizeUsd: parseUnits(notional.toFixed(18), 18),
    triggerPrice: parseUnits(trigger.toFixed(18), 18),
    limitPrice: parseUnits(price.toFixed(18), 18),
  };
}
