import * as React from "react";
import { cn } from "../lib/utils";
import { formatMoney } from "../lib/format";

export interface AmountTextProps extends React.HTMLAttributes<HTMLSpanElement> {
  value: number;
  currency?: string;
  locale?: string;
  decimals?: number;
  /** "$" by default; pass "" to hide. */
  symbol?: string;
  /**
   * "signed" — positive renders success green with +, negative destructive red.
   * "neutral" — always foreground, sign still shown.
   */
  colorMode?: "signed" | "neutral";
}

/** Signed money text with tabular figures, colored by sign. */
export function AmountText({
  value,
  currency,
  locale,
  decimals,
  symbol,
  colorMode = "signed",
  className,
  ...props
}: AmountTextProps) {
  return (
    <span
      className={cn(
        "font-semibold tabular-nums",
        colorMode === "signed" &&
          (value > 0 ? "text-success" : value < 0 ? "text-destructive" : "text-foreground"),
        colorMode === "neutral" && "text-foreground",
        className,
      )}
      {...props}
    >
      {formatMoney(value, { currency, decimals, locale, symbol })}
    </span>
  );
}
