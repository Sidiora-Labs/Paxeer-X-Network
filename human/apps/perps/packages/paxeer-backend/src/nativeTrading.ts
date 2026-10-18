import { parseUnits } from "viem";
import Decimal from "decimal.js";
import {
  PERPS_MARKETS,
  COLLATERAL_TOKENS,
  DIAMOND_ADDRESS,
} from "./core/perps/config";
import { paxeerChain } from "./core/chain";
import { getConnectedAddress } from "./core/viem/clients";
import { usePosition } from "./hooks/perps/usePosition";
import { ensureAllowance } from "./hooks/spot/useSpotApprovals";
import { usePerpsOrders } from "./hooks/perps/usePerpsOrders";
import { useQuoter } from "./hooks/perps/useQuoter";
import { refreshPositions } from "./hooks/perps/usePortfolioStats";
import { getNativeSnapshot, updateNativeSnapshot } from "./runtime";
import { toNativeSymbol } from "./nativeMetadata";
import { planNativeOrder, type NativeOrder } from "./nativeOrderPlan";
export { planNativeOrder, type NativeOrder } from "./nativeOrderPlan";
export async function executeNativeOrder(order: NativeOrder) {
  const state = getNativeSnapshot();
  const address = getConnectedAddress();
  if (
    !address ||
    !state.address ||
    address.toLowerCase() !== state.address.toLowerCase()
  )
    throw new Error("Connect your Paxeer wallet before trading.");
  if (state.chainId !== paxeerChain.id)
    throw new Error("Switch to Paxeer Mainnet before trading.");
  const nativeSymbol = toNativeSymbol(order.symbol);
  const live = state.markets.find(
    (row) => row.symbol.toUpperCase() === nativeSymbol,
  );
  const leverage = order.leverage ?? state.leverage[order.symbol] ?? 2;
  if (order.reduce_only) {
    if (
      !["BUY", "SELL"].includes(order.side?.toUpperCase()) ||
      order.order_type_ext ||
      order.visible_quantity !== undefined
    )
      throw new Error("Unsupported close execution instructions.");
    if ((order.order_type ?? order.type ?? "MARKET").toUpperCase() !== "MARKET")
      throw new Error("Use native position TP/SL for a conditional close.");
    const isLong = order.side.toUpperCase() === "SELL";
    const candidates = state.positions.filter(
      (position) =>
        PERPS_MARKETS.find(
          (market) => market.marketId === Number(position.marketId),
        )?.symbol === nativeSymbol &&
        Boolean(position.isLong) === isLong &&
        (!order.native_position_id ||
          String(position.positionId) === String(order.native_position_id)),
    );
    if (candidates.length !== 1)
      throw new Error(
        "Choose the native position to close from the Positions table.",
      );
    const position = candidates[0];
    const quantity = new Decimal(order.order_quantity ?? order.quantity ?? 0);
    const size = new Decimal(position.sizeUsd.toString()).div("1e18");
    const entry = new Decimal(position.entryPrice.toString()).div("1e18");
    const available = size.div(entry);
    if (!quantity.isFinite() || quantity.lte(0))
      throw new Error("Enter a positive close quantity.");
    const whole = Boolean(order.native_close_all) || quantity.eq(available);
    if (quantity.gt(available.plus("0.00000001")))
      throw new Error("Close quantity exceeds this position.");
    await useQuoter().quoteClosePosition(BigInt(position.positionId));
    const receipt = whole
      ? await usePosition().closePosition(BigInt(position.positionId))
      : await usePosition().partialClose(
          BigInt(position.positionId),
          parseUnits(quantity.mul(entry).toFixed(18), 18),
        );
    refreshPositions();
    if (typeof window !== "undefined")
      window.dispatchEvent(new Event("paxeer-network:transaction"));
    return {
      success: true,
      data: { transaction_hash: receipt.transactionHash },
      timestamp: Date.now(),
    };
  }
  const planned = planNativeOrder(order, live?.price ?? Number.NaN, leverage);
  if (!order.reduce_only && planned.type !== "MARKET")
    await ensureAllowance({
      token: planned.collateralToken,
      owner: address,
      spender: DIAMOND_ADDRESS,
      amount: planned.collateralAmount,
    });
  let receipt;
  if (planned.type === "MARKET") {
    const bound = takeNativeQuote(order, leverage);
    const {
      marketId,
      collateralToken,
      collateralAmount,
      leverage: quotedLeverage,
      isLong,
    } = bound.plan;
    receipt = await usePosition().openPosition({
      marketId,
      collateralToken,
      collateralAmount,
      leverage: quotedLeverage,
      isLong,
    });
  } else if (planned.type === "LIMIT") {
    receipt = await usePerpsOrders().placeLimitOrder(planned);
  } else if (
    ["STOP_LIMIT", "STOP_LOSS_LIMIT", "TAKE_PROFIT_LIMIT"].includes(
      planned.type,
    )
  ) {
    receipt = await usePerpsOrders().placeStopLimitOrder(planned);
  } else {
    throw new Error(
      `The Paxeer contracts do not support ${planned.type} through this order path.`,
    );
  }
  refreshPositions();
  if (typeof window !== "undefined")
    window.dispatchEvent(new Event("paxeer-network:transaction"));
  return {
    success: true,
    data: { transaction_hash: receipt.transactionHash },
    timestamp: Date.now(),
  };
}
export async function cancelNativeOrder(orderId: string) {
  if (!getConnectedAddress())
    throw new Error("Connect your wallet before cancelling an order.");
  const receipt = await usePerpsOrders().cancelOrder(BigInt(orderId));
  if (typeof window !== "undefined")
    window.dispatchEvent(new Event("paxeer-network:transaction"));
  return {
    success: true,
    data: { transaction_hash: receipt.transactionHash },
    timestamp: Date.now(),
  };
}

export const NATIVE_QUOTE_MAX_AGE_MS = 15_000;
type BoundNativeQuote = {
  plan: ReturnType<typeof planNativeOrder>;
  quote: import("./hooks/perps/useQuoter").OpenPositionQuote;
  timestamp: number;
  key: string;
  id: string;
};
const quotes = new Map<string, BoundNativeQuote>();
let quoteSequence = 0;
export function nativeOrderKey(order: NativeOrder, leverage: number) {
  const state = getNativeSnapshot();
  return JSON.stringify([
    state.address?.toLowerCase(),
    state.chainId,
    order.symbol,
    order.side?.toUpperCase(),
    order.order_type ?? order.type ?? "MARKET",
    new Decimal(order.order_quantity ?? order.quantity ?? 0).toString(),
    String(order.order_price ?? ""),
    String(order.trigger_price ?? ""),
    leverage,
    Boolean(order.reduce_only),
    order.collateral_token ?? "USDC",
  ]);
}
export async function prepareNativeQuote(
  order: NativeOrder,
  markPrice: number,
  leverage: number,
) {
  const key = nativeOrderKey(order, leverage);
  const plan = planNativeOrder(order, markPrice, leverage);
  const quote = await useQuoter().quoteOpenPosition(plan);
  if (nativeOrderKey(order, leverage) !== key)
    throw new Error("Wallet or network changed while requesting the quote.");
  const result = {
    plan,
    quote,
    timestamp: Date.now(),
    key,
    id: `native-quote-${Date.now()}-${++quoteSequence}`,
  };
  quotes.set(result.id, result);
  // Bound cache, never persist a trade quote between browser sessions.
  for (const [id, value] of quotes)
    if (Date.now() - value.timestamp > NATIVE_QUOTE_MAX_AGE_MS)
      quotes.delete(id);
  return result;
}
export function takeNativeQuote(
  order: NativeOrder,
  leverage: number,
  now = Date.now(),
) {
  const key = nativeOrderKey(order, leverage);
  const bound = order.native_quote_id
    ? quotes.get(order.native_quote_id)
    : undefined;
  if (
    !bound ||
    bound.key !== key ||
    now - bound.timestamp > NATIVE_QUOTE_MAX_AGE_MS
  )
    throw new Error(
      "Wait for a fresh quote matching this order before submitting.",
    );
  quotes.delete(bound.id);
  return bound;
}
