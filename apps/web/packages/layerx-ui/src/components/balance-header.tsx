"use client";

import * as React from "react";
import { Eye, EyeOff, TrendingUp, TrendingDown } from "lucide-react";
import { cn } from "../lib/utils";
import { formatBalance } from "../lib/format";

export interface BalanceHeaderProps {
  label?: string;
  value: number;
  symbol?: string;
  /** Daily change info line, e.g. { amount: "$234", percent: "+0.81%", up: true } */
  change?: { text: string; up: boolean };
  hidden?: boolean;
  onHiddenChange?: (hidden: boolean) => void;
  align?: "left" | "center";
  className?: string;
}

/**
 * Big balance with privacy eye toggle — the home/wallet header
 * in the design set ("$ 23,043.00" + 1-day change).
 */
export function BalanceHeader({
  label,
  value,
  symbol = "$",
  change,
  hidden: hiddenProp,
  onHiddenChange,
  align = "left",
  className,
}: BalanceHeaderProps) {
  const [internalHidden, setInternalHidden] = React.useState(false);
  const hidden = hiddenProp ?? internalHidden;
  const toggle = () => {
    const next = !hidden;
    setInternalHidden(next);
    onHiddenChange?.(next);
  };

  return (
    <div className={cn("flex flex-col gap-1", align === "center" && "items-center", className)}>
      {label && <span className="text-sm text-muted-foreground">{label}</span>}
      <div className="flex items-center gap-2.5">
        <span className="text-[32px] leading-none font-extrabold tabular-nums tracking-tight text-foreground">
          {hidden ? `${symbol} ••••••` : formatBalance(value, symbol)}
        </span>
        <button
          type="button"
          onClick={toggle}
          aria-label={hidden ? "Show balance" : "Hide balance"}
          className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors hover:bg-surface-sunken"
        >
          {hidden ? <EyeOff className="size-[18px]" /> : <Eye className="size-[18px]" />}
        </button>
      </div>
      {change && !hidden && (
        <span className="flex items-center gap-1.5 text-sm text-muted-foreground">
          1 day change:
          <span
            className={cn(
              "flex items-center gap-1 font-semibold",
              change.up ? "text-success" : "text-destructive",
            )}
          >
            {change.text}
            {change.up ? (
              <TrendingUp className="size-4" aria-hidden />
            ) : (
              <TrendingDown className="size-4" aria-hidden />
            )}
          </span>
        </span>
      )}
    </div>
  );
}
