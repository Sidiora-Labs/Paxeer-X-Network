import { createContext, useContext } from "react";
import type { API } from "@paxeer-network/types";

export interface SymbolContextState {
  base_dp: number;
  quote_dp: number;
  base_tick: number;
  quote_tick: number;
  base: string;
  quote: string;
  symbol: string;
  origin: API.SymbolExt;
  quote_min: number;
  quote_max: number;
}

export const SymbolContext = createContext<SymbolContextState>(
  {} as SymbolContextState,
);

export const useSymbolContext = () => {
  return useContext(SymbolContext);
};
