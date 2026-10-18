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
import { Box, cn } from "@paxeer-network/ui";
import { PaxeerNetworkChartTooltip } from "./customTooltip";
import { useColors } from "./useColors";

export type VolChartDataItem = {
  date: string;
  volume: number;
  opacity?: string | number;
};

export type VolChartTooltip = {
  rm?: number;
  dp?: number;
};

export type VolChartProps = {
  colors?: { fill: string };
  data: ReadonlyArray<VolChartDataItem> | VolChartDataItem[];
  tooltip?: VolChartTooltip;
  className?: string;
  invisible?: boolean;
  responsiveContainerProps?: Omit<ResponsiveContainerProps, "children">;
};

const RoundedRectangle = (props: any) => {
  const { fill, x, y, width, height, opacity } = props;
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
      opacity={opacity}
    />
  );
};

export const XAxisLabel: React.FC<any> = (props) => {
  const { x, y, payload, index, width, containerWidth } = props;
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
        fontSize={10}
        fill={"rgba(var(--oui-color-base-foreground)/0.54)"}
      >
        {index === 0 ? payload.value : t("chart.now")}
      </text>
    </g>
  );
};

const CustomizedCross = (props: any) => {
  const { width, height, payload, stroke, fill } = props;

  return (
    // @ts-ignore
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

const CustomTooltip = (
  props: TooltipProps<any, any> & { tooltip?: VolChartTooltip },
) => {
  const { active, payload, label, tooltip } = props;

  if (active && payload && payload.length) {
    return (
      <PaxeerNetworkChartTooltip
        label={label}
        value={payload[0].value}
        // prefix="Commission"
        titleClassName="oui-gap-4"
        rm={tooltip?.rm}
        dp={tooltip?.dp}
      />
    );
  }

  return null;
};

export const VolBarChart: React.FC<VolChartProps> = (props) => {
  const colors = useColors(
    props.colors?.fill
      ? { profit: props.colors?.fill, loss: props.colors?.fill }
      : undefined,
  );
  const widthRef = useRef<number | null>(null);

  const isEmpty =
    (props.data as any)?.reduce((pre: any, cur: any) => pre + cur.volume, 0) ===
    0;

  const maxVolume = (props.data as any)?.reduce(
    (pre: any, cur: any) => (pre > cur.volume ? pre : cur.volume),
    0,
  );

  const decimal = maxVolume <= 10 ? 2 : maxVolume <= 100 ? 1 : 0;

  return (
    // @ts-ignore
    <Box className={cn(props.className)}>
      {/* @ts-ignore */}
      <ResponsiveContainer
        className={cn(props.invisible && "chart-invisible")}
        onResize={(width) => {
          widthRef.current = width;
        }}
        {...props.responsiveContainerProps}
      >
        {/* @ts-ignore */}
        <BarChart
          data={props.data as any[]}
          style={{ direction: "ltr" }}
          margin={{ left: -10, top: 10, right: 10, bottom: 30 }}
        >
          {!props.invisible && (
            <Tooltip
              cursor={<CustomizedCross />}
              content={<CustomTooltip tooltip={props.tooltip} />}
            />
          )}
          <CartesianGrid
            vertical={false}
            stroke="rgb(var(--oui-color-line))"
            strokeOpacity={0.08}
            repeatCount={6}
          />
          {/* @ts-ignore */}
          <ReferenceLine y={0} stroke="rgb(var(--oui-color-base-10))" />
          {!props.invisible && (
            <Bar dataKey="volume" shape={<RoundedRectangle />}>
              {props.data.map((entry, index) => {
                return (
                  // @ts-ignore
                  <Cell
                    key={`cell-${index}`}
                    fill={entry.volume > 0 ? colors.profit : colors.loss}
                    opacity={entry.opacity}
                  />
                );
              })}
            </Bar>
          )}
          {/* @ts-ignore */}
          <YAxis
            tick={{
              fontSize: 10,
              fill: "rgba(var(--oui-color-base-foreground)/0.54)",
            }}
            tickLine={false}
            axisLine={false}
            dataKey={"volume"}
            tickFormatter={(value, index) => {
              if (isEmpty) return `${index * 100}`;
              return numberToHumanStyle(value, decimal);
            }}
            width={45}
          />
          {/* @ts-ignore */}
          <XAxis
            dataKey="date"
            tickLine={false}
            interval={Math.max(0, (props.data?.length ?? 0) - 2)}
            height={1}
            tick={(tickProps) => (
              <XAxisLabel
                {...tickProps}
                containerWidth={widthRef.current ?? tickProps.width}
              />
            )}
            stroke="rgb(var(--oui-color-line))"
            strokeOpacity={0.04}
          />
        </BarChart>
      </ResponsiveContainer>
    </Box>
  );
};
function numberToHumanStyle(number: number, decimalPlaces: number = 0): string {
  const abbreviations = ["", "K", "M", "B", "T"];

  let index = 0;
  while (number >= 1000 && index < abbreviations.length - 1) {
    number /= 1000;
    index++;
  }

  const roundedNumber = toFixedWithoutRounding(number, decimalPlaces);

  return `${roundedNumber}${abbreviations[index]}`;
}

function toFixedWithoutRounding(num: number, fix: number): string {
  const numStr = num.toString();
  const decimalIndex = numStr.indexOf(".");

  if (decimalIndex === -1 || fix === 0) {
    return numStr.split(".")[0];
  }

  const cutoffIndex = decimalIndex + fix + 1;

  return numStr.slice(0, cutoffIndex);
}
