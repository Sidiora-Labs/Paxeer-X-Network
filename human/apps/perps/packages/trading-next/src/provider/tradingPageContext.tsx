import { createContext, useContext } from "react";
import { TradingPageState } from "../types/types";

export const TradingPageContext = createContext({} as TradingPageState);

export const useTradingPageContext = () => {
  return useContext(TradingPageContext);
};
