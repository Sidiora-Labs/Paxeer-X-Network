import { useMemo } from "react";
import { OrderSide } from "@paxeer-network/types";
import { useMarkPrice } from "../../paxeer-network/useMarkPrice";

const useGetEstLiqPrice = (props: {
  estLiqPrice: number | null;
  symbol: string;
  side: OrderSide;
}) => {
  const { estLiqPrice, symbol, side } = props;

  const { data: markPrice } = useMarkPrice(symbol);

  return useMemo(() => {
    if (!estLiqPrice || !markPrice) {
      return null;
    }

    // long: if est. liq price is greater than mark price, don't display est. liq price
    // short: if est. liq price is less than mark price, don't display est. liq price
    if (side === OrderSide.BUY && estLiqPrice > markPrice) {
      return null;
    }
    if (side === OrderSide.SELL && estLiqPrice < markPrice) {
      return null;
    }

    return estLiqPrice;
  }, [estLiqPrice, markPrice, side]);
};

export { useGetEstLiqPrice };
