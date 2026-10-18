import { useMarketTradeStream, useSymbolsInfo } from "@paxeer-network/hooks";

export const useLastTradesScript = (symbol: string) => {
  const { data, isLoading } = useMarketTradeStream(symbol);
  //   const { quote, quote_dp, base, base_dp } = useContext(SymbolContext);
  const config = useSymbolsInfo()?.[symbol];
  const base = config?.("base");
  const quote = config?.("quote");
  const baseDp = config?.("base_dp");
  const quoteDp = config?.("quote_dp");
  //   console.log("base, quote", base, quote, "baseDp, quoteDp", baseDp, quoteDp);

  return {
    base,
    quote,
    data,
    isLoading,
    baseDp,
    quoteDp,
  };
};

export type LastTradesState = ReturnType<typeof useLastTradesScript>;
