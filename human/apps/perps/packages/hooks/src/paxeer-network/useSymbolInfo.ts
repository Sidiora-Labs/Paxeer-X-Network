import { useMemo } from "react";
import { useSymbolsInfo } from "./paxeerNetworkHooks";

export const useSymbolInfo = (symbol?: string) => {
  const infos = useSymbolsInfo();

  return useMemo(() => {
    return !symbol || infos.isNil ? null : infos[symbol];
  }, [infos, symbol]);
};
