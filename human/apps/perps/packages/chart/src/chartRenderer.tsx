import { FC, PropsWithChildren, useRef } from "react";
import { useChartContext } from "./hooks/chartContext";

export const ChartRenderer: FC<PropsWithChildren> = (props) => {
  const { size } = useChartContext();

  if (!size) return null;

  return (
    <svg viewBox={`0 0 ${size.width} ${size.height}`}>{props.children}</svg>
  );
};
