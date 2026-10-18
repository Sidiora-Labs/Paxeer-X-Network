import { ChartContextState } from "./hooks/chartContext";

export type ChartTheme = {
  bar: {
    tick: number;
    gap: number | ((data: any[], ctx: ChartContextState) => number);
  };
};

export const defaultTheme: ChartTheme = {
  bar: {
    tick: 4,
    gap: (data) => 2,
  },
};
