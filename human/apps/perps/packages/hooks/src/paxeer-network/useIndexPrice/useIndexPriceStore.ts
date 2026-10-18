import { create } from "zustand";

type IndexPriceStore = {
  indexPrices: Record<string, number>;
};

type IndexPriceActions = {
  updateIndexPrice: (indexPrice: Record<string, number>) => void;
  // getIndexPriceBySymbol: (symbol: string) => number;
};

const useIndexPriceStore = create<
  IndexPriceStore & {
    actions: IndexPriceActions;
  }
>((set) => ({
  indexPrices: {},
  actions: {
    updateIndexPrice: (indexPrice) => {
      set({
        indexPrices: indexPrice,
      });
    },
  },
}));

export { useIndexPriceStore };
