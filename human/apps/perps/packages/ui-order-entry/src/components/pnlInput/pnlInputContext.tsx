import { createContext, ReactNode, useContext } from "react";
import { PnLMode } from "./useBuilder.script";

export type PnlInputContextState = {
  mode: PnLMode;
  setMode: (mode: PnLMode) => void;
  tipsEle: ReactNode | null;
};

export const PnlInputContext = createContext<PnlInputContextState>(
  {} as PnlInputContextState,
);

export const usePnlInputContext = () => {
  return useContext(PnlInputContext);
};
