"use client";

import {
  AreaChart,
  Area,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ResponsiveContainer,
} from "recharts";
import { Skeleton } from "@/ui/atoms/skeleton";
import {
  ANALYTICS_CHART_COLORS,
  ANALYTICS_AXIS_FORMATTERS,
  TRACK_B_STRINGS,
} from "@/core/perps/config";
import { useOiSnapshots } from "@/hooks/perps/useAnalytics";

interface OiChartProps {
  marketId: number | undefined;
  isMobile: boolean;
}

interface ChartPoint {
  date: string;
  longOi: number;
  shortOi: number;
}

function parse18(val: string | null | undefined): number {
  if (!val) return 0;
  return parseFloat(val) / 1e18;
}

export function OiChart({ marketId, isMobile }: OiChartProps) {
  const { data, isLoading } = useOiSnapshots(marketId);

  if (isLoading || !data) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.OI_CHART_TITLE}
        </h3>
        <Skeleton className="w-full" style={{ height: isMobile ? 200 : 300 }} />
      </div>
    );
  }

  if (data.length === 0) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.OI_CHART_TITLE}
        </h3>
        <div
          className="w-full flex items-center justify-center text-xs text-muted-foreground"
          style={{ height: isMobile ? 200 : 300 }}
        >
          No OI data available
        </div>
      </div>
    );
  }

  const chartData: ChartPoint[] = data.map((s) => ({
    date: ANALYTICS_AXIS_FORMATTERS.shortDate(s.blockTimestamp),
    longOi: parse18(s.longOi),
    shortOi: parse18(s.shortOi),
  }));

  return (
    <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
      <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
        {TRACK_B_STRINGS.OI_CHART_TITLE}
      </h3>
      <div style={{ width: "100%", height: isMobile ? 200 : 300 }}>
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
            <XAxis
              dataKey="date"
              tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
              axisLine={false}
              tickLine={false}
              minTickGap={20}
            />
            <YAxis
              tick={{ fill: "hsl(var(--muted-foreground))", fontSize: 10 }}
              axisLine={false}
              tickLine={false}
              width={52}
              tickFormatter={ANALYTICS_AXIS_FORMATTERS.compactUsd}
            />
            <Tooltip
              contentStyle={{
                background: "hsl(var(--popover))",
                border: "1px solid hsl(var(--border))",
                borderRadius: 8,
                fontSize: 12,
              }}
              labelStyle={{ color: "hsl(var(--muted-foreground))" }}
              formatter={(value: number, name: string) => [
                ANALYTICS_AXIS_FORMATTERS.compactUsd(value),
                name === "longOi" ? "Long OI" : "Short OI",
              ]}
            />
            <Legend />
            <Area
              type="monotone"
              dataKey="longOi"
              name="Long OI"
              stroke={ANALYTICS_CHART_COLORS.oi}
              fill={ANALYTICS_CHART_COLORS.oi}
              fillOpacity={0.15}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
            <Area
              type="monotone"
              dataKey="shortOi"
              name="Short OI"
              stroke={ANALYTICS_CHART_COLORS.oiShort}
              fill={ANALYTICS_CHART_COLORS.oiShort}
              fillOpacity={0.15}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
