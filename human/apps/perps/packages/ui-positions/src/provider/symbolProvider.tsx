import { FC, PropsWithChildren, useMemo } from "react";
import { useSymbolsInfo } from "@paxeer-network/hooks";
import { SymbolContext, SymbolContextState } from "./symbolContext";

interface FormatterProviderProps {
  symbol: string;
}

export const SymbolProvider: FC<PropsWithChildren<FormatterProviderProps>> = (
  props,
) => {
  const { symbol, children } = props;
  const symbolInfo = useSymbolsInfo()[symbol];
  const memoizedValue = useMemo<SymbolContextState>(() => {
    return {
      symbol: symbol,
      base_dp: symbolInfo("base_dp"),
      quote_dp: symbolInfo("quote_dp"),
      base_tick: symbolInfo("base_tick"),
      quote_tick: symbolInfo("quote_tick"),
      base: symbolInfo("base"),
      quote: symbolInfo("quote"),
      origin: symbolInfo(),
      quote_max: symbolInfo("quote_max"),
      quote_min: symbolInfo("quote_min"),
    };
  }, [symbol, symbolInfo]);
  return (
    <SymbolContext.Provider value={memoizedValue}>
      {children}
    </SymbolContext.Provider>
  );
};
