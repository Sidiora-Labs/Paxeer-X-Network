import { SVGProps } from "react";

export type DateRange = {
  from: Date | undefined;
  to?: Date | undefined;
};

export type SummaryFilter = "All" | "1D" | "7D" | "30D";

export type BarDayFilter = "7" | "30" | "90";

export interface IconProps extends SVGProps<SVGSVGElement> {
  size?: number;
  className?: string;
}
