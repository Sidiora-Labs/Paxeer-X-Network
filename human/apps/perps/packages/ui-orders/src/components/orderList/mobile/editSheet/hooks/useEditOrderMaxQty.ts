import { useMemo } from "react";
import { useMaxQty } from "@paxeer-network/hooks";
import { API, MarginMode, OrderSide } from "@paxeer-network/types";

export function useEditOrderMaxQty(
  order: API.AlgoOrderExt,
  positionQty?: number,
) {
  const { reduce_only } = order;

  const maxQty = useMaxQty(order.symbol, order.side as OrderSide, {
    reduceOnly: reduce_only,
    marginMode: order.margin_mode ?? MarginMode.CROSS,
  });

  return useMemo(() => {
    if (reduce_only) {
      return Math.abs(positionQty ?? 0);
    }
    return order.quantity + Math.abs(maxQty);
  }, [order.quantity, maxQty, reduce_only, positionQty]);
}
