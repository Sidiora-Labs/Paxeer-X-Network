import { createContext, useContext } from "react";
import { Margin, Size } from "../constants/types";
import { ChartScale } from "./useChartState";

export type ChartContextState = {
  margin: Margin;
  size: Size;
  data: any[];
  scale: ChartScale;

  registerScale: (scale: ChartScale) => void;
};

export const ChartContext = createContext<ChartContextState>(
  {} as ChartContextState
);

export const useChartContext = () => {
  return useContext(ChartContext);
};
