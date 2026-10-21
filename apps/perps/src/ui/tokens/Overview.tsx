// Overview — 4 hero stat tiles (Total value / Unrealized / Open positions / Realized).
// TS port of src/components/design/Overview.jsx. Inline styles match prototype 1:1.
// Token dots pulled from an optional `openTokens` prop (falls back to prototype defaults).

import type { CSSProperties } from "react";
import { fmt } from "@/ui/utils/format";

export interface OverviewTokenDot {
  sym: string;
  color: string;
}

export interface OverviewProps {
  /** Total account value in USD (vault + unrealized). Pass null while loading. */
  totalValue: number | null;
  /** 24h delta on total value, USD. Pass null if unknown. */
  totalDelta24h: number | null;
  /** Sum of unrealized P&L across open positions, USD. Pass null while loading. */
  unrealized: number | null;
  /** Unrealized P&L as percent of total value. Pass null if unknown. */
  unrealizedPct: number | null;
  /** Number of currently open positions. */
  openCount: number;
  /** Token dots rendered on the Open Positions tile. Pass real market set — no fallback. */
  openTokens: OverviewTokenDot[];
  /** Lifetime realized P&L, USD. Pass null while loading. */
  realized: number | null;
  /** Lifetime trade count. */
  realizedTradeCount: number;
  className?: string;
  style?: CSSProperties;
}

const CARD: CSSProperties = {
  flex: 1,
  minWidth: 180,
  padding: "18px 20px",
  background: "var(--bg-subtle)",
  border: "1px solid var(--border)",
  borderRadius: 14,
  display: "flex",
  flexDirection: "column",
  gap: 8,
};

const LABEL: CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.14em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};

const VALUE: CSSProperties = {
  fontFamily: "var(--font-display)",
  fontSize: 28,
  fontWeight: 400,
  letterSpacing: "-0.02em",
  color: "var(--fg)",
};

const CAPTION_MONO: CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 10,
  color: "var(--fg-muted)",
};

export function Overview({
  totalValue,
  totalDelta24h,
  unrealized,
  unrealizedPct,
  openCount,
  openTokens,
  realized,
  realizedTradeCount,
  className,
  style,
}: OverviewProps) {
  const deltaPos = (totalDelta24h ?? 0) >= 0;
  const unrealPos = (unrealized ?? 0) >= 0;
  const realizedPos = (realized ?? 0) >= 0;
  const dash = "—";

  return (
    <div
      className={className}
      style={{ display: "flex", gap: 12, flexWrap: "wrap", width: "100%", ...style }}
    >
      <div style={CARD}>
        <div style={LABEL}>Total value</div>
        <div style={VALUE}>{totalValue === null ? dash : `$${fmt(totalValue, 2)}`}</div>
        <div style={{ ...CAPTION_MONO, color: totalDelta24h === null ? "var(--fg-muted)" : (deltaPos ? "var(--success)" : "var(--danger)") }}>
          {totalDelta24h === null
            ? `${dash} (24h)`
            : `${deltaPos ? "▲" : "▼"} $${fmt(Math.abs(totalDelta24h), 2)} (24h)`}
        </div>
      </div>
      <div style={CARD}>
        <div style={LABEL}>Unrealized P&amp;L</div>
        <div style={{ ...VALUE, color: unrealized === null ? "var(--fg)" : (unrealPos ? "var(--success)" : "var(--danger)") }}>
          {unrealized === null ? dash : `${unrealPos ? "+" : "−"}$${fmt(Math.abs(unrealized), 2)}`}
        </div>
        <div style={CAPTION_MONO}>
          {unrealizedPct === null
            ? `${dash} · across ${openCount}`
            : `${unrealPos ? "+" : ""}${unrealizedPct.toFixed(2)}% · across ${openCount}`}
        </div>
      </div>
      <div style={CARD}>
        <div style={LABEL}>Open positions</div>
        <div style={VALUE}>{openCount}</div>
        <div style={{ display: "flex", gap: 4, marginTop: 2, minHeight: 18 }}>
          {openTokens.slice(0, 6).map((t, i) => (
            <div
              key={`${t.sym}-${i}`}
              title={t.sym}
              style={{
                width: 18,
                height: 18,
                borderRadius: 999,
                background: t.color,
                border: "2px solid var(--bg-subtle)",
                marginLeft: i ? -6 : 0,
                display: "grid",
                placeItems: "center",
                color: "#fff",
                fontSize: 8,
                fontWeight: 600,
              }}
            >
              {t.sym[0]}
            </div>
          ))}
        </div>
      </div>
      <div style={CARD}>
        <div style={LABEL}>Realized P&amp;L</div>
        <div style={{ ...VALUE, color: realized === null ? "var(--fg)" : (realizedPos ? "var(--success)" : "var(--danger)") }}>
          {realized === null ? dash : `${realizedPos ? "+" : "−"}$${fmt(Math.abs(realized), 2)}`}
        </div>
        <div style={CAPTION_MONO}>lifetime · {realizedTradeCount} trades</div>
      </div>
    </div>
  );
}

export default Overview;
