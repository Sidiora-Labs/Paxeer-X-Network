import { useEffect, useState, useSyncExternalStore } from "react";
import {
  getNativeSnapshot,
  subscribeNativeSnapshot,
} from "@paxeer-network/paxeer-backend/runtime";
import {
  prepareNativeQuote,
  executeNativeOrder,
  nativeOrderKey,
  NATIVE_QUOTE_MAX_AGE_MS,
  planNativeOrder,
} from "@paxeer-network/paxeer-backend/nativeTrading";
import { formatUnits } from "viem";
import { toNativeSymbol } from "@paxeer-network/paxeer-backend/nativeMetadata";
export function useNativeOrderEntry(
  order: any,
  selectedLeverage: number | undefined,
  reset: () => void,
) {
  const state = useSyncExternalStore(
    subscribeNativeSnapshot,
    getNativeSnapshot,
    getNativeSnapshot,
  );
  const leverage = state.leverage[order.symbol] ?? selectedLeverage ?? 2;
  const [quote, setQuote] = useState<Awaited<
    ReturnType<typeof prepareNativeQuote>
  > | null>(null);
  const [isMutating, setIsMutating] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const markPrice =
    state.markets.find(
      (row) => row.symbol === toNativeSymbol(order.symbol ?? ""),
    )?.price ?? Number.NaN;
  let key = "";
  try {
    key = nativeOrderKey(order, leverage);
  } catch {}
  useEffect(() => {
    const timer = setInterval(() => setTick((t) => t + 1), 10_000);
    return () => clearInterval(timer);
  }, []);
  useEffect(() => {
    let cancelled = false;
    setQuote(null);
    setError(null);
    if (
      !state.address ||
      !Number(order.order_quantity) ||
      !Number.isFinite(markPrice) ||
      order.reduce_only
    )
      return;
    const timer = setTimeout(() => {
      void prepareNativeQuote(order, markPrice, leverage)
        .then((value) => {
          if (!cancelled) setQuote(value);
        })
        .catch((reason) => {
          if (!cancelled)
            setError(
              reason instanceof Error ? reason.message : "Quote unavailable.",
            );
        });
    }, 300);
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [key, markPrice, tick]);
  const current =
    quote?.key === key &&
    Date.now() - quote.timestamp <= NATIVE_QUOTE_MAX_AGE_MS
      ? quote
      : null;
  const validate = async () => {
    planNativeOrder(order, markPrice, leverage);
    return order;
  };
  const submit = async (options?: { resetOnSuccess?: boolean }) => {
    if (error) throw new Error(error);
    if (
      !order.reduce_only &&
      (order.order_type ?? "MARKET") === "MARKET" &&
      !current
    )
      throw new Error("Wait for a fresh Paxeer quote before submitting.");
    if (order.tp_trigger_price || order.sl_trigger_price)
      throw new Error(
        "Attach TP/SL to the confirmed native position after opening it.",
      );
    setIsMutating(true);
    try {
      const result = await executeNativeOrder({
        ...order,
        leverage,
        native_quote_id: current?.id,
      });
      if (options?.resetOnSuccess !== false) reset();
      return result;
    } finally {
      setIsMutating(false);
    }
  };
  const freeCollateral = Number(state.walletBalances.USDC ?? 0);
  const maxQty =
    Number.isFinite(markPrice) && markPrice > 0
      ? (freeCollateral * leverage) / markPrice
      : 0;
  const estLiqPrice = current
    ? Number(formatUnits(current.quote.liquidationPrice, 18))
    : null;
  const estSlippage = current
    ? Number(formatUnits(current.quote.priceImpact, 18))
    : null;
  return {
    submit,
    helper: {
      validate,
      validator: validate,
      getProjectedUSDCBorrow: () => null,
    },
    isMutating,
    freeCollateral,
    maxQty,
    maxQtys: { maxBuy: maxQty, maxSell: maxQty },
    estLiqPrice,
    estLiqPriceDistance:
      estLiqPrice && markPrice
        ? Math.abs(estLiqPrice - markPrice) / markPrice
        : null,
    estSlippage,
    estLeverage: leverage,
    symbolLeverage: leverage,
  };
}
