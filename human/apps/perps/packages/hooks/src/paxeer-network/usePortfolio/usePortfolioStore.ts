import { create } from "zustand";
import { devtools } from "zustand/middleware";
import { immer } from "zustand/middleware/immer";

type AccountPortfolioState = {
  totalCollateral: number;
  freeCollateral: number;
  totalValue: number;
  availableBalance: number;
  unsettledPnL: number;
  maxLeverage: number;
};

export type PortfolioStore = {} & AccountPortfolioState;

export type PortfolioActions = {
  updatePortfolio: (portfolio: AccountPortfolioState) => void;
};

const usePortfolioStore = create<
  PortfolioStore & { actions: PortfolioActions }
>()(
  devtools(
    immer((set) => ({
      totalCollateral: 0,
      freeCollateral: 0,
      totalValue: 0,
      availableBalance: 0,
      unsettledPnL: 0,
      maxLeverage: 0,
      actions: {
        updatePortfolio: (portfolio: Partial<AccountPortfolioState>) => {
          set(
            (state) => {
              Object.keys(portfolio).forEach((key) => {
                if (key in state) {
                  state[key as keyof AccountPortfolioState] = portfolio[
                    key as keyof AccountPortfolioState
                  ] as number;
                }
              });
            },
            false,
            "updatePortfolio",
          );
        },
      },
    })),
  ),
);

const useTotalCollateral = () =>
  usePortfolioStore((state) => state.totalCollateral);
const useFreeCollateral = () =>
  usePortfolioStore((state) => state.freeCollateral);
const useTotalValue = () => usePortfolioStore((state) => state.totalValue);
const useAvailableBalance = () =>
  usePortfolioStore((state) => state.availableBalance);
const useUnsettledPnL = () => usePortfolioStore((state) => state.unsettledPnL);
const useMaxLeverage = () => usePortfolioStore((state) => state.maxLeverage);

const usePortfolioActions = () => usePortfolioStore((state) => state.actions);
const portfolioActions = usePortfolioActions;

export {
  usePortfolioStore,
  useTotalValue,
  useFreeCollateral,
  useTotalCollateral,
  useAvailableBalance,
  useUnsettledPnL,
  useMaxLeverage,
  usePortfolioActions,
  portfolioActions,
};
