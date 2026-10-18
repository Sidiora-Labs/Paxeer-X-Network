import { useMemoizedFn } from "..";
import { useIndexPriceStore } from "./useIndexPrice/useIndexPriceStore";

export const useIndexPricesStream = () => {
  const indexPrices = useIndexPriceStore((state) => state.indexPrices);
  const getIndexPrice = (token: string) => {
    if (token === "USDC") {
      return 1;
    }
    return indexPrices[`PERP_${token}_USDC`] ?? 0;
  };
  return {
    data: indexPrices,
    getIndexPrice: useMemoizedFn(getIndexPrice),
  };
};
