import { FC, useEffect, useRef } from "react";
import { axisBottom, axisLeft } from "d3-axis";
import { select } from "d3-selection";
import { useChartContext } from "../hooks/chartContext";

// import
export type AxisProps = {
  // direction?: "x" | "y";
  orientation?: "top" | "bottom" | "left" | "right";
  tickValues?: any[];
};

export const Axis: FC<AxisProps> = (props) => {
  const { orientation = "bottom" } = props;
  const { scale, size, margin } = useChartContext();

  const gRef = useRef<SVGGElement | null>(null);

  useEffect(() => {
    if (!scale || !gRef.current) return;
    let translate, axis;

    switch (orientation) {
      case "left":
        translate = `translate(${margin.left}, ${margin.top})`;
        axis = axisLeft(scale.y)
          .ticks(6)
          .tickSizeInner(-(size.width - margin.left - margin.right))
          .tickSizeOuter(5)

          .tickPadding(10);

        break;

      default:
        translate = `translate(${margin.left}, ${size.height - margin.bottom})`;
        axis = axisBottom(scale.x).ticks(2);
        if (typeof props.tickValues !== "undefined") {
          axis.tickValues(props.tickValues);
        }
    }

    select(gRef.current)
      .attr("transform", translate)
      .call(axis)
      .call((g) => g.select(".domain").remove())
      .call((g) =>
        g
          .selectAll(".tick line")
          .attr("stroke", "rgb(var(--oui-color-line) / 0.08)"),
      )
      .call((g) =>
        g
          .selectAll(".tick text")
          .attr("fill", "rgba(var(--oui-color-base-foreground)/0.54)")
          .attr("font-size", 10),
      );
  }, [scale, orientation]);

  return <g ref={gRef} className="oui-chart-axis"></g>;
};
