import { FC, PropsWithChildren, useMemo } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { TradingPageState, TradingPageProps } from "../types/types";
import { getBasicSymbolInfo } from "../utils/utils";
import { TradingPageContext } from "./tradingPageContext";

/**
 * Provider does not import a default layout strategy; consumer or desktop UI
 * should pass layoutStrategy via props or use a fallback when rendering LayoutHost.
 */
export const TradingPageProvider: FC<PropsWithChildren<TradingPageProps>> = (
  props,
) => {
  const { symbol, children } = props;
  const symbolInfo = useSymbolsInfo()[symbol];

  const memoizedValue = useMemo<TradingPageState>(() => {
    const basicSymbol = getBasicSymbolInfo(symbolInfo);
    return {
      ...props,
      layoutStrategy: props.layoutStrategy,
      getInitialLayout: props.getInitialLayout,
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
