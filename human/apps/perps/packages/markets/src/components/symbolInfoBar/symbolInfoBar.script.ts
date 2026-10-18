import { useMemo } from "react";
import {
  useGetRwaSymbolInfo,
  useSymbolsInfo,
  useTickerStream,
} from "@paxeer-network/hooks";

export type UseSymbolInfoBarScriptOptions = {
  symbol: string;
};

export type UseSymbolInfoBarScriptReturn = ReturnType<
  typeof useSymbolInfoBarScript
>;

export function useSymbolInfoBarScript(options: UseSymbolInfoBarScriptOptions) {
  const { symbol } = options;
  const { isRwa, open, closeTimeInterval, openTimeInterval } =
    useGetRwaSymbolInfo(symbol);

  const data = useTickerStream(symbol);

  const symbolsInfo = useSymbolsInfo();

  const leverage = useMemo(() => {
    const info = symbolsInfo[symbol];
    const baseImr = info("base_imr");
    return getLeverage(baseImr);
  }, [symbol, symbolsInfo]);

  return {
    symbol,
    data,
    leverage,
    isRwa,
    open,
    closeTimeInterval,
    openTimeInterval,
  };
}

function getLeverage(base_imr: number) {
  return base_imr ? 1 / base_imr : undefined;
}
