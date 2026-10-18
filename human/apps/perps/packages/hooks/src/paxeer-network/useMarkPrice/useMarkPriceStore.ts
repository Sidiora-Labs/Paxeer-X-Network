import { create } from "zustand";

type MarkPriceStore = {
  markPrices: Record<string, number>;
  // ask_bid: number[];
  // orderBook: Record<string, number>;
};

type MarkPriceActions = {
  updateMarkPrice: (markPrice: Record<string, number>) => void;
  getMarkPriceBySymbol: (symbol: string) => number;
};

const useMarkPriceStore = create<
  MarkPriceStore & { actions: MarkPriceActions }
>((set, get) => ({
  markPrices: {},
  // orderBook: {},
  // ask_bid: [],

  actions: {
    updateMarkPrice: (markPrice) => {
      set({
        markPrices: markPrice,
      });
    },
    getMarkPriceBySymbol: (symbol: string) => {
      return get().markPrices[symbol];
    },
  },
}));

const useMarkPrices = () => useMarkPriceStore((state) => state.markPrices);
const useMarkPriceBySymbol = (symbol: string) =>
  useMarkPriceStore((state) => state.actions.getMarkPriceBySymbol(symbol));
const useMarkPriceActions = () => useMarkPriceStore((state) => state.actions);

export {
  useMarkPriceStore,
  useMarkPrices,
  useMarkPriceBySymbol,
  useMarkPriceActions,
};
