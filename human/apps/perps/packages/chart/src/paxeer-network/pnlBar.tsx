import React, { useRef } from "react";
import {
  BarChart,
  XAxis,
  YAxis,
  ResponsiveContainer,
  Bar,
  Cell,
  Tooltip,
  CartesianGrid,
  ReferenceLine,
  Cross,
} from "recharts";
import type { TooltipProps } from "recharts";
import type { Props as ResponsiveContainerProps } from "recharts/types/component/ResponsiveContainer";
import { useTranslation } from "@paxeer-network/i18n";
import { cn } from "@paxeer-network/ui";
import { tickFormatter } from "../utils/yTickFormatter";
import { PaxeerNetworkChartTooltip } from "./customTooltip";
import { useColors } from "./useColors";

export type PnLChartDataItem = {
  date: string;
  pnl: number;
};

export type PnLChartProps = {
  colors?: {
    profit: string;
    loss: string;
  };
  data: ReadonlyArray<PnLChartDataItem> | PnLChartDataItem[];
  invisible?: boolean;
  responsiveContainerProps?: Omit<ResponsiveContainerProps, "children">;
};

const RoundedRectangle: React.FC<any> = (props) => {
  const { fill, x, y, width, height } = props;

  const absHeight = Math.abs(height);

  return (
    <rect
      rx={2}
      x={x}
      y={height > 0 ? y : y + height}
      width={width}
      height={absHeight}
      stroke="none"
      fill={fill}
    />
  );
};

export const XAxisLabel: React.FC<any> = (props) => {
  const { x, y, stroke, payload, index, width, containerWidth } = props;
  const { t } = useTranslation();
  const _x =
    index === 0
      ? 48
      : containerWidth > 0
        ? containerWidth - 10
        : width + payload.offset;

  return (
    <g transform={`translate(${_x},${y - 6})`}>
      <text
        x={0}
        y={0}
        dy={16}
        textAnchor={index === 0 ? "start" : "end"}
        direction="ltr"
        unicodeBidi="plaintext"
        // textAnchor={"start"}
        fontSize={10}
        fill={"rgba(var(--oui-color-base-foreground)/0.54)"}
      >
        {index === 0 ? payload.value : t("chart.now")}
      </text>
    </g>
  );
};

const CustomizedCross: React.FC<any> = (props) => {
  const { width, height, stroke, fill } = props;
  return (
    <Cross
      // y={props.y + props.top}
      x={props.x + props.width / 2}
      top={props.top}
      height={height}
      width={1}
      stroke={"rgba(var(--oui-color-base-foreground)/0.16)"}
      strokeDasharray={"3 2"}
      fill={"none"}
    />
  );
};

const CustomTooltip: React.FC<TooltipProps<any, any>> = (props) => {
  const { active, payload, label } = props;
  const todayStr = useRef(new Date().toISOString().split("T")[0]);
  const { t } = useTranslation();

  if (active && payload && payload.length) {
    return (
      <PaxeerNetworkChartTooltip
        label={label === todayStr.current ? t("chart.now") : label}
        value={payload[0].value}
        coloring
      />
    );
  }

  return null;
};

export const PnLBarChart: React.FC<PnLChartProps> = (props) => {
  const { invisible, data, responsiveContainerProps } = props;
  const colors = useColors(props.colors);
  const widthRef = useRef<number>(0);
  return (
    <ResponsiveContainer
      className={cn(invisible && "chart-invisible")}
      onResize={(width) => {
        widthRef.current = width;
      }}
      {...responsiveContainerProps}
    >
      <BarChart
        data={data as any[]}
        style={{ direction: "ltr" }}
        margin={{ left: -10, top: 10, right: 10, bottom: 30 }}
      >
        {!invisible && (
          <Tooltip
            // cursor={{ fillOpacity: 0.1 }}
            cursor={<CustomizedCross />}
            content={<CustomTooltip />}
          />
        )}

        <CartesianGrid
          vertical={false}
          stroke="rgb(var(--oui-color-line))"
          strokeOpacity={0.04}
        />

        <ReferenceLine y={0} stroke="rgb(var(--oui-color-line) / 0.04)" />

        {!invisible && (
          <Bar dataKey="pnl" shape={<RoundedRectangle />}>
            {data.map((entry, index) => {
              return (
                <Cell
                  key={`cell-${index}`}
                  fill={entry.pnl > 0 ? colors.profit : colors.loss}
                />
              );
            })}
          </Bar>
        )}

        <YAxis
          tick={{
            fontSize: 10,
            fill: "rgba(var(--oui-color-base-foreground)/0.54)",
          }}
          tickFormatter={(value) => tickFormatter(value)}
          tickLine={false}
          axisLine={false}
          dataKey={"pnl"}
        />
        <XAxis
          dataKey="date"
          // axisLine={false}
          tickLine={false}
          interval={data.length - 2}
          // tick={renderQuarterTick}
          height={1}
          // scale="time"
          // tick={{ fontSize: 10, fill: "rgba(255,255,255,0.54)" }}
          tick={<XAxisLabel containerWidth={widthRef.current} />}
          stroke="rgb(var(--oui-color-line))"
          strokeOpacity={0.04}
        />
      </BarChart>
    </ResponsiveContainer>
  );
};
