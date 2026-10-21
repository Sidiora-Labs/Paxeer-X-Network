import * as React from "react";
import { cn } from "../lib/utils";

/** Big number over a muted label — "18 / Total referrals". */
export function Stat({
  value,
  label,
  className,
  align = "center",
}: {
  value: React.ReactNode;
  label: React.ReactNode;
  className?: string;
  align?: "left" | "center";
}) {
  return (
    <div
      className={cn(
        "flex flex-col gap-1",
        align === "center" ? "items-center text-center" : "items-start",
        className,
      )}
    >
      <span className="text-2xl font-bold tabular-nums text-foreground">{value}</span>
      <span className="text-sm text-muted-foreground">{label}</span>
    </div>
  );
}

/** Two stats separated by a vertical hairline, as on Earning / Network. */
export function StatPair({
  left,
  right,
  className,
}: {
  left: { value: React.ReactNode; label: React.ReactNode };
  right: { value: React.ReactNode; label: React.ReactNode };
  className?: string;
}) {
  return (
    <div className={cn("grid grid-cols-2", className)}>
      <Stat value={left.value} label={left.label} />
      <Stat value={right.value} label={right.label} className="border-l border-border" />
    </div>
  );
}
