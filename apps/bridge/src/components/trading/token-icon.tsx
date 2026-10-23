import { useState, type ReactNode } from "react";

import { parseSymbol } from "@/lib/trading";
import { cn } from "@/lib/utils";

const SIZES = {
  "2xs": "size-4 text-[7px]",
  xs: "size-5 text-[8px]",
  sm: "size-6 text-[9px]",
  md: "size-8 text-[11px]",
  lg: "size-10 text-xs",
  xl: "size-12 text-sm",
} as const;

export type TokenIconSize = keyof typeof SIZES;

/** A stable hue from a string, so each token keeps its colour across renders. */
function hueOf(text: string) {
  let h = 0;
  for (let i = 0; i < text.length; i++) h = (h * 31 + text.charCodeAt(i)) % 360;
  return h;
}

export interface TokenIconProps {
  /** Token ticker (`BTC`) or a full symbol (`PERP_BTC_USDC`) — the base asset is used. */
  symbol: string;
  /** Image URL. Falls back to a lettered disc if omitted or it fails to load. */
  src?: string;
  size?: TokenIconSize;
  /** Square with rounded corners instead of a circle — used for chains. */
  shape?: "circle" | "square";
  className?: string;
}

/** A token logo with a deterministic, lettered fallback. */
export function TokenIcon({ symbol, src, size = "sm", shape = "circle", className }: TokenIconProps) {
  const token = symbol.includes("_") || symbol.includes("-") || symbol.includes("/") ? parseSymbol(symbol).base : symbol.toUpperCase();
  const [failed, setFailed] = useState(false);
  const hue = hueOf(token);
  return (
    <span
      data-slot="token-icon"
      className={cn(
        "relative inline-flex shrink-0 items-center justify-center overflow-hidden font-semibold select-none",
        shape === "circle" ? "rounded-full" : "rounded-[30%]",
        SIZES[size],
        className,
      )}
      style={src && !failed ? undefined : { background: `oklch(0.62 0.14 ${hue})`, color: "white" }}
    >
      {src && !failed ? (
        <img src={src} alt="" className="size-full object-cover" onError={() => setFailed(true)} />
      ) : (
        <span aria-hidden="true">{token.slice(0, token.length > 3 ? 2 : 3)}</span>
      )}
      <span className="sr-only">{token}</span>
    </span>
  );
}

export interface ChainIconProps extends Omit<TokenIconProps, "symbol" | "shape"> {
  /** Chain name, e.g. `Arbitrum`. */
  name: string;
}

/** A rounded-square chain badge with the same lettered fallback. */
export function ChainIcon({ name, ...props }: ChainIconProps) {
  return <TokenIcon symbol={name.replace(/\s+/g, "").slice(0, 4)} shape="square" {...props} />;
}

export interface SymbolDisplayProps {
  /** `PERP_BTC_USDC`, `BTC-PERP`, `BTC/USDC` … */
  symbol: string;
  /** How much of the symbol to show. */
  format?: "base" | "base-quote" | "base-type";
  /** Show the token icon before the name. */
  showIcon?: boolean;
  iconSrc?: string;
  size?: TokenIconSize;
  /** Trailing badge — leverage, `RWA`, `New`… */
  badge?: ReactNode;
  className?: string;
}

/** Token icon + ticker, the way markets, positions and orders label an instrument. */
export function SymbolDisplay({ symbol, format = "base-type", showIcon = true, iconSrc, size = "xs", badge, className }: SymbolDisplayProps) {
  const { base, quote, type } = parseSymbol(symbol);
  return (
    <span data-slot="symbol-display" className={cn("inline-flex min-w-0 items-center gap-1.5 font-medium", className)}>
      {showIcon && <TokenIcon symbol={base} src={iconSrc} size={size} />}
      <span className="truncate">
        {base}
        {format === "base-quote" && quote && <span className="text-muted-foreground">/{quote}</span>}
        {format === "base-type" && type === "PERP" && <span className="text-muted-foreground">-PERP</span>}
      </span>
      {badge != null && <SymbolBadge>{badge}</SymbolBadge>}
    </span>
  );
}

/** A tiny tonal pill for leverage, market type or listing status. */
export function SymbolBadge({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <span className={cn("inline-flex h-4 items-center rounded-[5px] bg-container-high px-1 text-[10px] leading-none font-medium text-muted-foreground", className)}>
      {children}
    </span>
  );
}
