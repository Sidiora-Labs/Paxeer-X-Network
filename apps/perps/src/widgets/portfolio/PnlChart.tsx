"use client";

import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  ResponsiveContainer,
  ReferenceLine,
} from "recharts";
import type { DailyPnl } from "@/hooks/perps/useDailyPnl";
import { fmtUsdCompact as fmtUsd } from "@/ui/utils/format";

interface PnlChartProps {
  dailyPnl: DailyPnl[];
}

interface ChartPoint {
  date: string;
  cumPnl: number;
}

export function PnlChart({ dailyPnl }: PnlChartProps) {
  // ── Empty state (guards against 0 data points) ────────────────────
  if (!dailyPnl || dailyPnl.length === 0) {
    return (
      <div className="w-full h-48 flex items-center justify-center text-xs text-muted-foreground">
        No PnL history yet
      </div>
    );
  }

  // ── Compute cumulative PnL curve ──────────────────────────────────
  let running = 0;
  const data: ChartPoint[] = dailyPnl.map((d) => {
    running += parseFloat(d.pnl || "0");
    return {
      date: d.date.length >= 10 ? d.date.slice(5, 10) : d.date, // "MM-DD"
      cumPnl: running,
    };
  });

  const latestCum = data[data.length - 1].cumPnl;
  const isPositive = latestCum >= 0;
  const color = isPositive ? "#22c55e" : "#ef4444";

  return (
    <div className="w-full h-48">
      <ResponsiveContainer width="100%" height="100%">
        <LineChart
          data={data}
          margin={{ top: 8, right: 8, bottom: 0, left: 0 }}
        >
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
            width={48}
            tickFormatter={(v: number) => {
              const abs = Math.abs(v);
              if (abs >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
              if (abs >= 1_000) return `${(v / 1_000).toFixed(1)}K`;
              return v.toFixed(0);
            }}
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
            formatter={(value: number) => [fmtUsd(value), "Cumulative PnL"]}
          />
          <Line
            type="monotone"
            dataKey="cumPnl"
            stroke={color}
            strokeWidth={2}
            dot={false}
            isAnimationActive={false}
          />
        </LineChart>
      </ResponsiveContainer>
    </div>
  );
}
