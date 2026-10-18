import { FC, useEffect, useRef } from "react";
import { scaleBand, scaleLinear } from "d3-scale";
import { max, min } from "d3-array";
import { ChartContextState, useChartContext } from "../hooks/chartContext";

import { select } from "d3-selection";

export type BarProps = {
  color?: string | ((d: any, ctx: ChartContextState) => string);
};

export const Bar: FC<BarProps> = (props) => {
  // const {color} = props;
  const { data, scale, margin, size, registerScale } = useChartContext();
  const gRef = useRef<SVGGElement | null>(null);

  useEffect(() => {
    // create a y scale for the bar chart
    if (!data || !size) return;

    const y0 = max([
      Math.abs(min(data, (d) => d.pnl)),
      Math.abs(max(data, (d) => d.pnl)),
    ]);

    const y = scaleLinear()
      //   .domain([min(data, (d) => d.pnl), max(data, (d) => d.pnl)])
      .domain([-y0, y0])
      .range([size.height - margin.bottom - margin.top, 0])
      .nice();

    const x = scaleBand(
      data.map((d) => new Date(d.date)),
      [margin.left, size.width - margin.right]
    ).paddingInner(0.2);

    registerScale({
      x,
      y,
    });
  }, [data, size]);

  useEffect(() => {
    if (!scale || !gRef.current) return;

    const u = select(gRef.current)
      .attr("transform", `translate(0, ${margin.top})`)
      .selectAll()
      .data(data);

    u.join("rect")
      // .transition()
      .attr("fill", props.color || "steelblue")
      .attr("rx", 2)
      .attr("x", (d) => scale.x(new Date(d.date)))
      .attr("y", (d) => scale.y(Math.max(0, d.pnl)))
      .attr("height", (d) => Math.abs(scale.y(d.pnl) - scale.y(0)))
      .attr("width", scale.x.bandwidth());

    u.exit().remove();
  }, [data, scale]);

  return <g ref={gRef}></g>;
};
