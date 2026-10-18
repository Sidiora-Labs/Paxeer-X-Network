import type { API } from "@paxeer-network/types";
import type { BasicSymbolInfo } from "../types/types";

type ValueOf<T> = T[keyof T];

export const getBasicSymbolInfo = (
  symbolInfo: (
    key?: keyof API.SymbolExt,
    defaultValue?: ValueOf<API.SymbolExt>,
  ) => any,
): BasicSymbolInfo => {
  return {
    base_dp: symbolInfo("base_dp"),
    quote_dp: symbolInfo("quote_dp"),
    base_tick: symbolInfo("base_tick"),
    base: symbolInfo("base"),
    quote: symbolInfo("quote"),
  };
};
