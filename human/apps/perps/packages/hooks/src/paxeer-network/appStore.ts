import { create } from "zustand";
import { immer } from "zustand/middleware/immer";
import { API } from "@paxeer-network/types";
import { WSMessage } from "@paxeer-network/types";
import { Decimal, zero } from "@paxeer-network/utils";
import { mergeHoldingBalance } from "../utils/mergeHoldingBalance";

// import { devtools } from "zustand/middleware";

export type AppStatus = {
  positionsLoading: boolean;
  ordersLoading: boolean;
  fundingRatesLoading: boolean;
  ready: boolean;
};

export type Portfolio = {
  holding?: API.Holding[];
  totalCollateral: Decimal;
  freeCollateral: Decimal;
  freeCollateralUSDCOnly: Decimal;
  totalValue: Decimal | null;
  availableBalance: number;
  unsettledPnL: number;
  totalUnrealizedROI: number;
  usdcHolding: number;
};

export type AppState = {
  accountInfo?: API.AccountInfo;
  // positions: API.PositionExt[];
  symbolsInfo?: Record<string, API.SymbolExt>;
  tokensInfo?: API.Token[];
  rwaSymbolsInfo?: Record<string, API.RwaSymbol>;
  fundingRates?: Record<string, API.FundingRate>;
  portfolio: Portfolio;
  appState: AppStatus;
};

export type AppActions = {
  cleanAll: () => void;
  setAccountInfo: (accountInfo: API.AccountInfo) => void;
  setTokensInfo: (tokensInfo: API.Token[]) => void;
  // setPositions: (positions: API.PositionExt[]) => void;
  setSymbolsInfo: (symbolsInfo: Record<string, API.SymbolExt>) => void;
  setRwaSymbolsInfo: (rwaSymbolsInfo: Record<string, API.RwaSymbol>) => void;
  setFundingRates: (fundingRates: Record<string, API.FundingRate>) => void;
  updateAppStatus: (key: keyof AppStatus, value: boolean) => void;
  updatePortfolio: (
    key: keyof Omit<Portfolio, "usdc" | "holding">,
    value: number | Decimal,
  ) => void;

  batchUpdateForPortfolio: (data: Partial<Portfolio>) => void;
  restoreHolding: (holding: API.Holding[]) => void;
  updateHolding: (msg: Record<string, WSMessage.Holding>) => void;
};

/**
 * @warning This store should be used with caution. It contains sensitive account and portfolio data.
 * Please ensure you have proper authorization and follow security best practices when using this store.
 *
 * @example
 * // Correct usage:
 * const accountInfo = useAppStore(state => state.accountInfo);
 *
 * // Avoid direct store manipulation:
 * const store = useAppStore.getState(); // Not recommended
 */
export const useAppStore = create<
  AppState & {
    actions: AppActions;
  }
  //   [["zustand/devtools", never], ["zustand/immer", never]]
>()(
  immer((set) => ({
    // accountInfo: null,
    portfolio: {
      totalCollateral: zero,
      totalValue: null,
      freeCollateral: zero,
      freeCollateralUSDCOnly: zero,
      availableBalance: 0,
      unsettledPnL: 0,
      totalUnrealizedROI: 0,
      usdcHolding: 0,
    },
    appState: {
      positionsLoading: false,
      ordersLoading: false,
      fundingRatesLoading: false,
      ready: false,
    } as AppStatus,
    actions: {
      setTokensInfo: (tokensInfo: API.Token[]) => {
        set((state) => {
          state.tokensInfo = tokensInfo;
        }, false);
      },
      cleanAll: () => {
        set((state) => {
          state.accountInfo = undefined;
          state.portfolio = {
            totalCollateral: zero,
            totalValue: null,
            freeCollateral: zero,
            freeCollateralUSDCOnly: zero,
            availableBalance: 0,
            unsettledPnL: 0,
            totalUnrealizedROI: 0,
            usdcHolding: 0,
          };
        }, false);
      },
      setAccountInfo: (accountInfo: API.AccountInfo) => {
        set(
          (state) => {
            state.accountInfo = accountInfo;
          },
          false,
          // "setAccountInfo"
        );
      },
      setSymbolsInfo: (symbolsInfo: Record<string, API.SymbolExt>) => {
        set(
          (state) => {
            state.symbolsInfo = symbolsInfo;
          },
          false,
          // "setSymbolsInfo"
        );
      },
      setRwaSymbolsInfo: (rwaSymbolsInfo: Record<string, API.RwaSymbol>) => {
        set(
          (state) => {
            state.rwaSymbolsInfo = rwaSymbolsInfo;
          },
          false,
          // "setRwaSymbolsInfo"
        );
      },
      setFundingRates: (fundingRates: Record<string, API.FundingRate>) => {
        set(
          (state) => {
            state.fundingRates = fundingRates;
          },
          false,
          // "setFundingRates"
        );
      },
      updateAppStatus: (key: keyof AppStatus, value: boolean) => {
        set(
          (state) => {
            state.appState[key] = value;
          },
          false,
          // "updateAppStatus"
        );
      },
      updatePortfolio: (
        key: keyof Omit<Portfolio, "usdc" | "holding">,
        value: any,
      ) => {
        set(
          (state) => {
            state.portfolio[key] = value;
          },
          false,
          // "updatePortfolio"
        );
      },
      batchUpdateForPortfolio: (data: Partial<Portfolio>) => {
        set(
          (state) => {
            state.portfolio = { ...state.portfolio, ...data };
          },
          false,
          // "batchUpdateForPortfolio"
        );
      },
      restoreHolding: (holding: API.Holding[]) => {
        set(
          (state) => {
            state.portfolio.holding = holding;
          },
          false,
          // "updateHolding"
        );
      },
      updateHolding(msg) {
        set(
          (state) => {
            if (state.portfolio.holding && state.portfolio.holding.length) {
              for (const key in msg) {
                const holding = state.portfolio.holding.find(
                  (item) => item.token === key,
                );
                if (holding) {
                  Object.assign(
                    holding,
                    mergeHoldingBalance(holding, msg[key]),
                  );
                }
                // else {
                //   state.portfolio.holding.push({
                //     token: key,
                //     holding: msg[key].holding,
                //     frozen: msg[key].frozen,
                //   });
                // }
              }
            }
          },
          false,
          // "updateHolding"
        );
      },
    },
  })),
);

export const useAccountInfo = () => useAppStore((state) => state.accountInfo);

export const usePortfolio = () => useAppStore((state) => state.portfolio);

export const useFundingRateBySymbol = (symbol: string) =>
  useAppStore((state) => state.fundingRates?.[symbol]);
