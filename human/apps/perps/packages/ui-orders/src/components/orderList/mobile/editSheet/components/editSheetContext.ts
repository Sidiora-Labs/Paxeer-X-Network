import { createContext, useContext } from "react";
import { OrderValidationResult } from "@paxeer-network/hooks";
import { PaxeerNetworkOrder } from "@paxeer-network/types";
import { SymbolContextState } from "../../../../provider/symbolContext";

export type EditSheetContextState = {
  symbolInfo: SymbolContextState;
  getErrorMsg: (
    key: keyof OrderValidationResult,
    customValue?: string,
  ) => string;
  setOrderValue: (key: keyof PaxeerNetworkOrder, value: any) => void;
};

export const EditSheetContext = createContext({} as EditSheetContextState);

export function useEditSheetContext() {
  return useContext(EditSheetContext);
}
