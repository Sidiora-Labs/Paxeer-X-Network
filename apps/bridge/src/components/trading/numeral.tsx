import type { ReactNode } from "react";

import { formatNumber, toneOf, TONE_TEXT, type NumberRule } from "@/lib/trading";
import { cn } from "@/lib/utils";

export interface NumeralProps {
  /** The value. `null`, `undefined` and non-finite numbers render the placeholder. */
  value: number | null | undefined;
  /** How to format: price, number, percent (value is a fraction), compact or usd. */
  rule?: NumberRule;
  /** Decimal places. Wins over `tick`. */
  dp?: number;
  /** Price increment, e.g. 0.01 — sets decimal places when `dp` is omitted. */
  tick?: number;
  /** Colour by sign: green for positive, red for negative. */
  coloring?: boolean;
  /** Prefix positive values with `+`. */
  showSign?: boolean;
  /** Keep trailing zeros. */
  pad?: boolean;
  /** Rendered before the number, e.g. `$`. */
  prefix?: ReactNode;
  /** Rendered after the number in a muted tone, e.g. `USDC`. */
  unit?: ReactNode;
  /** Shown when the value is missing. */
  placeholder?: ReactNode;
  /** Mask the value — for "hide balances" toggles. */
  hidden?: boolean;
  /** Rendered instead of the value when `hidden`. */
  mask?: ReactNode;
  className?: string;
  unitClassName?: string;
}

/** A formatted, tabular number with optional sign colouring, units and masking. */
export function Numeral({
  value,
  rule = "number",
  dp,
  tick,
  coloring,
  showSign,
  pad,
  prefix,
  unit,
  placeholder = "—",
  hidden,
  mask = "*****",
  className,
  unitClassName,
}: NumeralProps) {
  const valid = value != null && Number.isFinite(value);
  const body = hidden ? mask : valid ? formatNumber(value, rule, { dp, tick, sign: showSign, pad }) : placeholder;
  return (
    <span
      data-slot="numeral"
      className={cn("inline-flex items-baseline gap-1 font-mono tabular-nums", coloring && !hidden && TONE_TEXT[toneOf(valid ? value : 0)], className)}
    >
      <span>
        {prefix}
        {body}
      </span>
      {unit != null && <span className={cn("text-[0.85em] text-muted-foreground", unitClassName)}>{unit}</span>}
    </span>
  );
}

export interface StatisticProps {
  label: ReactNode;
  children: ReactNode;
  /** Align label and value to the start or end. */
  align?: "start" | "end";
  className?: string;
}

/** A small label over a value — the building block of info bars and summaries. */
export function Statistic({ label, children, align = "start", className }: StatisticProps) {
  return (
    <div data-slot="statistic" className={cn("flex min-w-0 flex-col gap-0.5", align === "end" ? "items-end text-right" : "items-start", className)}>
      <span className="text-[11px] whitespace-nowrap text-muted-foreground">{label}</span>
      <span className="text-sm font-medium whitespace-nowrap text-foreground">{children}</span>
    </div>
  );
}
