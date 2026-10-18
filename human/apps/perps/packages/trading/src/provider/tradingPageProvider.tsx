import { FC, PropsWithChildren, useMemo } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { TradingPageState, TradingPageProps } from "../types/types";
import { getBasicSymbolInfo } from "../utils/utils";
import { TradingPageContext } from "./tradingPageContext";

export const TradingPageProvider: FC<PropsWithChildren<TradingPageProps>> = (
  props,
) => {
  const { symbol, children } = props;
  const symbolInfo = useSymbolsInfo()[symbol];

  const memoizedValue = useMemo<TradingPageState>(() => {
    const basicSymbol = getBasicSymbolInfo(symbolInfo);
    return {
      ...props,
      symbolInfo: {
        ...basicSymbol,
        symbol: symbol,
      },
    };
  }, [props, symbol, symbolInfo]);
  return (
    <TradingPageContext.Provider value={memoizedValue}>
      {children}
    </TradingPageContext.Provider>
  );
};
