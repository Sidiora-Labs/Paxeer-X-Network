"use client";

import { useMemo } from "react";
import { Skeleton } from "@/ui/atoms/skeleton";
import { useFees } from "@/hooks/perps/useFees";
import {
  FEE_TYPE_LABELS,
  TRACK_B_STRINGS,
  ANALYTICS_CHART_COLORS,
} from "@/core/perps/config";
import { fmtUsdCompact as fmtUsd, weiOrZero, weiPct, weiToNumber } from "@/ui/utils/format";

interface FeesBreakdownCardProps {
  userAddress: string | undefined;
}

const FEE_TYPE_COLORS: Record<number, string> = {
  0: ANALYTICS_CHART_COLORS.markPrice,
  1: ANALYTICS_CHART_COLORS.indexPrice,
  2: ANALYTICS_CHART_COLORS.funding,
  3: ANALYTICS_CHART_COLORS.oiShort,
};

interface FeeGroup {
  feeType: number;
  label: string;
  totalWei: bigint;
  count: number;
}

/** Fees grouped by type with exact wei totals, largest first. */
export function groupFees(data: readonly { feeType: number; feeTypeName?: string | null; feeUsd: string | null | undefined }[]): FeeGroup[] {
  const map = new Map<number, FeeGroup>();
  for (const fee of data) {
    const wei = weiOrZero(fee.feeUsd);
    const existing = map.get(fee.feeType);
    if (existing) {
      existing.totalWei += wei;
      existing.count += 1;
    } else {
      map.set(fee.feeType, {
        feeType: fee.feeType,
        label: FEE_TYPE_LABELS[fee.feeType] ?? fee.feeTypeName ?? `Type ${fee.feeType}`,
        totalWei: wei,
        count: 1,
      });
    }
  }
  return Array.from(map.values()).sort((a, b) => (b.totalWei > a.totalWei ? 1 : b.totalWei < a.totalWei ? -1 : 0));
}

export function FeesBreakdownCard({ userAddress }: FeesBreakdownCardProps) {
  const { data, isLoading } = useFees(
    userAddress ? { userAddress } : undefined
  );

  const groups = useMemo<FeeGroup[]>(() => (data && data.length > 0 ? groupFees(data) : []), [data]);

  const totalFeesWei = useMemo(() => groups.reduce((sum, g) => sum + g.totalWei, 0n), [groups]);
  const totalFees = weiToNumber(totalFeesWei);

  if (!userAddress) {
    return null;
  }

  if (isLoading) {
    return (
      <div className="rounded-[var(--radius-lg)] bg-surface-1 p-4">
        <p className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium mb-3">
          {TRACK_B_STRINGS.FEES_CARD_TITLE}
        </p>
        <div className="space-y-2">
          <Skeleton className="h-6 w-32" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-full" />
        </div>
      </div>
    );
  }

  if (groups.length === 0) {
    return (
      <div className="rounded-[var(--radius-lg)] bg-surface-1 p-4">
        <p className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium mb-3">
          {TRACK_B_STRINGS.FEES_CARD_TITLE}
        </p>
        <p className="text-sm text-muted-foreground">{TRACK_B_STRINGS.FEES_EMPTY}</p>
      </div>
    );
  }

  return (
    <div className="rounded-[var(--radius-lg)] bg-surface-1 p-4">
      <p className="text-[10px] text-muted-foreground uppercase tracking-wider font-medium mb-1">
        {TRACK_B_STRINGS.FEES_CARD_TITLE}
      </p>
      <p className="text-xl font-bold font-mono text-red-400 mb-4">
        -{fmtUsd(totalFees)}
      </p>

      {/* Bar breakdown */}
      {totalFeesWei > 0n && (
        <div className="flex h-2 rounded-full overflow-hidden mb-3">
          {groups.map((g) => (
            <div
              key={g.feeType}
              className="h-full"
              style={{
                width: `${weiPct(g.totalWei, totalFeesWei) ?? 0}%`,
                backgroundColor: FEE_TYPE_COLORS[g.feeType] ?? "hsl(var(--muted-foreground))",
              }}
            />
          ))}
        </div>
      )}

      {/* Fee rows */}
      <div className="space-y-2">
        {groups.map((g) => (
          <div key={g.feeType} className="flex items-center justify-between text-sm">
            <div className="flex items-center gap-2">
              <div
                className="w-2.5 h-2.5 rounded-full shrink-0"
                style={{
                  backgroundColor: FEE_TYPE_COLORS[g.feeType] ?? "hsl(var(--muted-foreground))",
                }}
              />
              <span className="text-muted-foreground">{g.label}</span>
              <span className="text-xs text-muted-foreground">({g.count})</span>
            </div>
            <span className="font-mono font-medium text-red-400">
              -{fmtUsd(weiToNumber(g.totalWei))}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}
