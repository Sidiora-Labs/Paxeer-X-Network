/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useId, useMemo, useRef } from "react";
import {
  XAxis,
  YAxis,
  CartesianGrid,
  AreaChart,
  Area,
  Tooltip,
  ResponsiveContainer,
} from "recharts";
import type { TooltipProps } from "recharts";
import type { Props as ResponsiveContainerProps } from "recharts/types/component/ResponsiveContainer";
import { useTranslation } from "@paxeer-network/i18n";
import { useScreen } from "@paxeer-network/ui";
import { tickFormatter } from "../utils/yTickFormatter";
import { PaxeerNetworkChartTooltip } from "./customTooltip";
import { useColors } from "./useColors";
import { XAxisLabel } from "./xAxisLabel";

export type VolumeAreaChartProps = {
  data: any;
  invisible?: boolean;
  responsiveContainerProps?: Omit<ResponsiveContainerProps, "children">;
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
        coloring={false}
      />
    );
  }

  return null;
};

const dataTransfer = (data: any[]) => {
  const series: any[] = [];
  data?.reduce<any>((acc, item) => {
    acc += item.perp_volume ?? 0;
    series.push({ ...item, volume: acc });
    return acc;
  }, 0);
  return series;
};

export const VolumeAreaChart: React.FC<VolumeAreaChartProps> = (props) => {
  const { responsiveContainerProps } = props;
  const colors = useColors();

  const { isMobile } = useScreen();

  const colorId = useId();

  const data = useMemo(() => dataTransfer(props.data), [props.data]);

  const baseValue = useMemo(
    () => data.map((item) => item.volume).sort((a, b) => a - b)[0] ?? 0,
    [data],
  );

  const chartComponent = (
    <AreaChart
      data={data}
      style={{ direction: "ltr" }}
      margin={{ top: 20, right: 10, left: -10, bottom: 0 }}
    >
      <CartesianGrid
        vertical={false}
        stroke="rgb(var(--oui-color-line))"
        strokeOpacity={0.04}
      />
      <XAxis
        dataKey="date"
        interval={props.data?.length ? props.data.length - 2 : 0}
        tick={<XAxisLabel />}
        stroke="rgb(var(--oui-color-line))"
        strokeOpacity={0.04}
      />
      <YAxis
        dataKey="volume"
        tick={{
          fontSize: 10,
          fill: "rgba(var(--oui-color-base-foreground)/0.54)",
        }}
        tickLine={false}
        axisLine={false}
        tickFormatter={tickFormatter}
      />
      {!props.invisible && (
        <Tooltip
          cursor={{ strokeDasharray: "3 2", strokeOpacity: 0.16 }}
          content={<CustomTooltip />}
        />
      )}
      {!props.invisible && (
        <>
          <defs>
            <linearGradient id={colorId} x1="0" y1="0" x2="0" y2="1">
              <stop stopColor={colors.primary} offset="0%" stopOpacity={0.5} />
              <stop stopColor={colors.primary} offset="100%" stopOpacity={0} />
            </linearGradient>
          </defs>
          <Area
            type="monotone"
            dataKey="volume"
            stroke={colors.primary}
            strokeWidth={isMobile ? 1.5 : 2}
            dot={false}
            isAnimationActive={false}
            fill={`url(#${colorId})`}
            baseValue={baseValue || 0}
          />
        </>
      )}
    </AreaChart>
  );

  return (
    <ResponsiveContainer
      className={props.invisible ? "chart-invisible" : undefined}
      {...responsiveContainerProps}
    >
      {chartComponent}
    </ResponsiveContainer>
  );
};
