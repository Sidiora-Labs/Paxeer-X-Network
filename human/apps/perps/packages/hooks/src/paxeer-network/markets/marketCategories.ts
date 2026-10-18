import {
  createContext,
  useContext,
  FC,
  PropsWithChildren,
  createElement,
  ReactNode,
} from "react";
import { API } from "@paxeer-network/types";
import type { RwaSymbolsInfo } from "../useRwaSymbolsInfo";
import type { SymbolsInfo } from "../useSymbolsInfo";

export type MarketCategoryComponentKey =
  | "marketsSheet"
  | "expandMarkets"
  | "dropDownMarkets"
  | "subMenuMarkets"
  | "marketsDataList"
  | "horizontalMarkets";

export type MarketBuiltInTabType =
  | "favorites"
  | "community"
  | "all"
  | "crypto"
  | "rwa"
  | "preTge"
  | "newListing"
  | "recent";

type MarketTabBase = {
  name?: string;
  icon?: ReactNode | string;
  suffix?: ReactNode | string;
  isVisible?: (
    symbolList: API.MarketInfoExt[],
    ctx: {
      rwaSymbolsInfo?: RwaSymbolsInfo;
      symbolsInfo?: SymbolsInfo;
    },
  ) => boolean;
};

export type BuiltInMarketTab = MarketTabBase & {
  type: MarketBuiltInTabType;
};

export type CustomMarketTab = MarketTabBase & {
  id: string;
  name: string;
  match: (market: API.MarketInfoExt) => boolean;
};

export type MarketTabConfig = BuiltInMarketTab | CustomMarketTab;

export interface MarketCategoryContext {
  componentKey: MarketCategoryComponentKey;
  builtIn: Record<MarketBuiltInTabType, BuiltInMarketTab>;
}

/**
 * Function-only config for market tabs.
 *
 * @param original - Default built-in tabs for the current component (as MarketTabConfig[])
 * @param context  - { componentKey, builtIn } for referencing built-in tab definitions
 * @returns Final tab sequence to render
 */
export type MarketCategoryConfig = (
  original: MarketTabConfig[],
  context: MarketCategoryContext,
) => MarketTabConfig[];

const MarketCategoriesConfigContext = createContext<
  MarketCategoryConfig | undefined
>(undefined);

export type MarketCategoriesConfigProviderProps = {
  value?: MarketCategoryConfig;
};

export const MarketCategoriesConfigProvider: FC<
  PropsWithChildren<MarketCategoriesConfigProviderProps>
> = ({ value, children }) => {
  return createElement(
    MarketCategoriesConfigContext.Provider,
    { value },
    children,
  );
};

export function useMarketCategoriesConfig(): MarketCategoryConfig | undefined {
  return useContext(MarketCategoriesConfigContext);
}
