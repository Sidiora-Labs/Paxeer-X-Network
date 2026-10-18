import { useEffect, useState } from "react";
import {
  scaleLinear,
  scaleUtc,
  type ScaleLinear,
  type ScaleTime,
  type ScaleBand,
} from "d3-scale";
import { max, min, extent } from "d3-array";
import { Margin, Size } from "../constants/types";

export type ChartScale = {
  x: ScaleTime<any, any, any> | ScaleLinear<any, any, any> | ScaleBand<any>;
  y: ScaleLinear<any, any, any>;
};

export const useChartState = (options: {
  data: any[];
  size: Size;
  margin: Margin;
  x: string;
  y: string;
}) => {
  const { size, margin, data, x: xField, y: yField } = options;
  const [scale, setScale] = useState<ChartScale | undefined>();

  if (!xField || !yField) {
    throw new Error("Please provider x or y parmater");
  }

  useEffect(() => {
    if (!size || !data) return;

    const x = scaleUtc(
      extent(data, (d) => new Date(d[xField])),
      [margin.left, size.width - margin.right]
    );

    const y = scaleLinear(
      [0, max(data, (d) => d[yField])],
      [size.height - margin.bottom, margin.top]
    );

    setScale({ x, y });
  }, [data, size]);

  //   console.log("scale", scale);

  return {
    scale,
  };
};
