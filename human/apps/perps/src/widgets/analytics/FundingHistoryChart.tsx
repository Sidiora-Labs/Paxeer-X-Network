"use client";

import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  Legend,
  ReferenceLine,
  ResponsiveContainer,
} from "recharts";
import { Skeleton } from "@/ui/atoms/skeleton";
import {
  ANALYTICS_CHART_COLORS,
  ANALYTICS_AXIS_FORMATTERS,
  TRACK_B_STRINGS,
} from "@/core/perps/config";
import { useMarketSnapshots } from "@/hooks/perps/useAnalytics";
import { formatSignedFundingDecimalPct, fundingWadDecimal, parseFundingRateWad } from "@/core/perps/fundingRate";
import { FUNDING_LABEL_ONCHAIN_24H } from "./MarketSnapshotTable";

interface FundingHistoryChartProps {
  marketId: number | undefined;
  isMobile: boolean;
}

interface ChartPoint {
  date: string;
  /** The exact on-chain rate at 18 decimals; null when the snapshot has none, drawn as a gap. */
  rate: bigint | null;
  /** The plot coordinate only, from plotFundingRate; labels print from `rate`. */
  fundingRate: number | null;
}

/** The chart boundary: an exact 18-decimal rate as the y coordinate recharts draws. Never used for text. */
function plotFundingRate(rate: bigint): number {
  const whole = rate / 10n ** 18n;
  return Number(whole) + Number(rate - whole * 10n ** 18n) / 1e18;
}

export function FundingHistoryChart({ marketId, isMobile }: FundingHistoryChartProps) {
  const { data, isLoading } = useMarketSnapshots(marketId);

  if (isLoading || !data) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.FUNDING_CHART_TITLE}
        </h3>
        <Skeleton className="w-full" style={{ height: isMobile ? 200 : 300 }} />
      </div>
    );
  }

  if (data.length === 0) {
    return (
      <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
        <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
          {TRACK_B_STRINGS.FUNDING_CHART_TITLE}
        </h3>
        <div
          className="w-full flex items-center justify-center text-xs text-muted-foreground"
          style={{ height: isMobile ? 200 : 300 }}
        >
          No funding data available
        </div>
      </div>
    );
  }

  const chartData: ChartPoint[] = data.map((s) => {
    const rate = parseFundingRateWad(s.fundingRate24h);
    return {
      date: ANALYTICS_AXIS_FORMATTERS.shortDate(s.blockTimestamp),
      rate,
      fundingRate: rate === null ? null : plotFundingRate(rate),
    };
  });

  return (
    <div className="rounded-card bg-surface-1 p-5 shadow-04 transition-colors ease-data duration-200">
      <h3 className="font-mono text-xs-label uppercase tracking-label text-foreground/50 mb-4">
        {TRACK_B_STRINGS.FUNDING_CHART_TITLE}
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
              tickFormatter={ANALYTICS_AXIS_FORMATTERS.percent}
            />
            <ReferenceLine y={0} stroke="hsl(var(--border))" strokeDasharray="3 3" />
            <Tooltip
              contentStyle={{
                background: "hsl(var(--popover))",
                border: "1px solid hsl(var(--border))",
                borderRadius: 8,
                fontSize: 12,
              }}
              labelStyle={{ color: "hsl(var(--muted-foreground))" }}
              formatter={(_value: number, _name: string, item: { payload?: ChartPoint }) => [
                formatSignedFundingDecimalPct(fundingWadDecimal(item.payload?.rate ?? null)),
                FUNDING_LABEL_ONCHAIN_24H,
              ]}
            />
            <Legend />
            <Line
              type="monotone"
              dataKey="fundingRate"
              name={FUNDING_LABEL_ONCHAIN_24H}
              stroke={ANALYTICS_CHART_COLORS.funding}
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
