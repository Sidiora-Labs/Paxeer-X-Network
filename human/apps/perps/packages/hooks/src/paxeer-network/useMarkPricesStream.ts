import { useMarkPriceStore } from "./useMarkPrice/useMarkPriceStore";

export const useMarkPricesStream = () => {
  const data = useMarkPriceStore((state) => state.markPrices);

  return { data };
};
