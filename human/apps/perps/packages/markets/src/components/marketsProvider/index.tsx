import {
  FC,
  createContext,
  PropsWithChildren,
  useState,
  useContext,
  useCallback,
  useMemo,
} from "react";
import type { API, RouterAdapter } from "@paxeer-network/types";
import { LeftNavProps } from "@paxeer-network/ui-scaffold";
import type { MarketTabConfig } from "../../type";

type MarketsContextState = {
  symbol?: string;
  searchValue?: string;
  onSearchValueChange?: (searchValue: string) => void;
  clearSearchValue?: () => void;
  tabs?: MarketTabConfig[];
} & MarketsProviderProps;

export const MarketsContext = createContext<MarketsContextState>({});

export type MarketsProviderProps = {
  symbol?: string;
  onSymbolChange?: (symbol: API.Symbol) => void;
  // only for mobile
  navProps?: {
    logo?: {
      src: string;
      alt: string;
    };
    routerAdapter?: RouterAdapter;
    leftNav?: LeftNavProps;
  };
  comparisonProps?: {
    /**
     * Set Name of Exchanges in the comparison list.
     * @default 'paxeer-network'
     */
    exchangesName?: string;
    /**
     * Set Icon URL of Exchanges in the comparison list.
     * @default ""
     */
    exchangesIconSrc?: string;
  };
  tabs?: MarketTabConfig[];
};

export const MarketsProvider: FC<PropsWithChildren<MarketsProviderProps>> = (
  props,
) => {
  const { symbol, comparisonProps, children, onSymbolChange, tabs } = props;
  const [searchValue, setSearchValue] = useState("");

  const clearSearchValue = useCallback(() => {
    setSearchValue("");
  }, []);

  const memoizedValue = useMemo<MarketsContextState>(() => {
    return {
      searchValue,
      onSearchValueChange: setSearchValue,
      clearSearchValue,
      symbol: symbol,
      onSymbolChange: onSymbolChange,
      comparisonProps: comparisonProps,
      tabs: tabs,
    };
  }, [
    searchValue,
    symbol,
    onSymbolChange,
    setSearchValue,
    comparisonProps,
    tabs,
  ]);

  return (
    <MarketsContext.Provider value={memoizedValue}>
      {children}
    </MarketsContext.Provider>
  );
};

export const useMarketsContext = () => {
  return useContext<MarketsContextState>(MarketsContext);
};
