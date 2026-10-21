"use client";

import {
  LineChart,
  Line,
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
import { useMarkPriceHistory } from "@/hooks/perps/useAnalytics";

interface MarkPriceChartProps {
  marketId: number | undefined;
  isMobile: boolean;
}

interface ChartPoint {
  date: string;
  markPrice: number;
  indexPrice: number;
}

function parse18(val: string | null | undefined): number {
  if (!val) return 0;
  return parseFloat(val) / 1e18;
}

export function MarkPriceChart({ marketId, isMobile }: MarkPriceChartProps) {
  const { data, isLoading } = useMarkPriceHistory(marketId);

  if (isLoading || !data) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.MARK_PRICE_CHART_TITLE}
        </h3>
        <Skeleton className="w-full" style={{ height: isMobile ? 200 : 300 }} />
      </div>
    );
  }

  if (data.length === 0) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.MARK_PRICE_CHART_TITLE}
        </h3>
        <div
          className="w-full flex items-center justify-center text-xs text-muted-foreground"
          style={{ height: isMobile ? 200 : 300 }}
        >
          No price data available
        </div>
      </div>
    );
  }

  const chartData: ChartPoint[] = data.map((r) => ({
    date: ANALYTICS_AXIS_FORMATTERS.shortDate(r.blockTimestamp),
    markPrice: parse18(r.markPrice),
    indexPrice: parse18(r.indexPrice),
  }));

  return (
    <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
      <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
        {TRACK_B_STRINGS.MARK_PRICE_CHART_TITLE}
      </h3>
      <div style={{ width: "100%", height: isMobile ? 200 : 300 }}>
        <ResponsiveContainer width="100%" height="100%">
          <LineChart data={chartData} margin={{ top: 8, right: 8, bottom: 0, left: 0 }}>
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
                name === "markPrice" ? "Mark Price" : "Index Price",
              ]}
            />
            <Legend />
            <Line
              type="monotone"
              dataKey="markPrice"
              name="Mark Price"
              stroke={ANALYTICS_CHART_COLORS.markPrice}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
            <Line
              type="monotone"
              dataKey="indexPrice"
              name="Index Price"
              stroke={ANALYTICS_CHART_COLORS.indexPrice}
              strokeWidth={2}
              dot={false}
              isAnimationActive={false}
            />
          </LineChart>
        </ResponsiveContainer>
      </div>
    </div>
  );
}
