import { FC, useEffect, useMemo, useState } from "react";
import { line, curveBasis } from "d3-shape";
import { scaleLinear, scaleUtc } from "d3-scale";
import { max, extent, min } from "d3-array";
import { useChartContext } from "../hooks/chartContext";

export type LineProps = {
  color?: string;
  symbol?: string;
  dataKey: string;
  dataTransform?: (data: any) => any;
};

export const Line: FC<LineProps> = (props) => {
  const { symbol, color, dataKey } = props;
  const { data, scale, size, margin, registerScale } = useChartContext();
  // const [path, setPath] = useState<string | undefined>();
  //

  useEffect(() => {
    if (!size || !data) return;

    const transform =
      typeof props.dataTransform === "function"
        ? props.dataTransform
        : (d: any) => d;

    const _data = transform(data);

    const x = scaleUtc(
      extent(_data, (d) => new Date(d.date)),
      [0, size.width - margin.right - margin.left]
    );

    const y = scaleLinear(
      [min(_data, (d) => d[dataKey]), max(_data, (d) => d[dataKey])],
      // [size.height - margin.bottom - margin.top, margin.top]
      [size.height - margin.bottom - margin.top, 0]
    ).nice();

    registerScale({ x, y });
  }, [data, size]);

  const lineGenerator = useMemo(() => {
    if (!scale) return () => undefined;
    return line()
      .x((d) => scale.x(new Date(d.date)))
      .y((d) => scale.y(d[dataKey]))
      .curve(curveBasis);
  }, [scale]);

  const d = useMemo(() => {
    if (typeof lineGenerator !== "function") return undefined;
    return lineGenerator(data);
  }, [data, lineGenerator]);

  if (!d) return null;

  return (
    <g transform={`translate(${margin.left}, ${margin.top})`}>
      <path d={d} fill="none" stroke={color ?? "white"} strokeWidth={2} />
    </g>
  );
};
