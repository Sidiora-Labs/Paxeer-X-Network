import { useCallback, useMemo } from "react";
import { account } from "@paxeer-network/perp";
import type { API } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { useCollateral, useHoldingStream, useIndexPricesStream } from "..";
import { useAppStore } from "./appStore";

const { LTV, collateralRatio } = account;

interface LTVOptions {
  input?: number;
  token?: string;
}

export const useComputedLTV = (options: LTVOptions = {}) => {
  const { input, token } = options;

  const isUSDC = token?.toUpperCase() === "USDC";

  const tokensInfo = useAppStore((state) => state.tokensInfo);

  const { usdc, data: holdingList = [] } = useHoldingStream();

  const { getIndexPrice } = useIndexPricesStream();

  const { unsettledPnL } = useCollateral();

  const usdcBalance = useMemo<number>(() => {
    if (isUSDC && input) {
      return new Decimal(usdc?.holding ?? 0).add(input).toNumber();
    }
    return usdc?.holding ?? 0;
  }, [usdc?.holding, input, isUSDC]);

  const getAdjustedQty = useCallback(
    (item: API.Holding) => {
      if (input && item.token === token) {
        return new Decimal(item?.holding ?? 0).add(input).toNumber();
      }
      return item?.holding ?? 0;
    },
    [input, token],
  );

  const memoizedLTV = useMemo<number>(() => {
    return LTV({
      usdcBalance: usdcBalance,
      upnl: unsettledPnL,
      assets: holdingList
        .filter((h) => h.token.toUpperCase() !== "USDC")
        .map((item) => {
          const indexPrice = getIndexPrice(item.token);
          const findToken = tokensInfo?.find((i) => i.token === item.token);
          const qty = getAdjustedQty(item);
          const weight = collateralRatio({
            baseWeight: findToken?.base_weight ?? 0,
            discountFactor: findToken?.discount_factor ?? 0,
            collateralCap: findToken?.user_max_qty ?? qty,
            collateralQty: qty,
            indexPrice: indexPrice,
          });
          return {
            qty: qty,
            indexPrice: indexPrice,
            weight: weight.toNumber(),
          };
        }),
    });
  }, [
    usdcBalance,
    unsettledPnL,
    holdingList,
    tokensInfo,
    getIndexPrice,
    getAdjustedQty,
  ]);

  if (new Decimal(usdcBalance).add(new Decimal(unsettledPnL)).gte(zero)) {
    return 0;
  }

  return new Decimal(memoizedLTV)
    .mul(100)
    .toDecimalPlaces(2, Decimal.ROUND_DOWN)
    .toNumber();
};
