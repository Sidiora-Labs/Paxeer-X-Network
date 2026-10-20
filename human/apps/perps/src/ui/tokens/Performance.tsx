// Performance — 7D PnL / Volume / ROI mini-tiles + Daily PnL bar chart.
// TS port of src/components/design/Performance.jsx. Inline styles match prototype 1:1.

import type { CSSProperties } from "react";

export interface PerformanceProps {
  /** 7D realized P&L in USD. Pass null while loading. */
  sevenDPnl: number | null;
  /** 7D P&L vs previous 7D period as percent. Pass null if unknown. */
  sevenDPnlVsPrevPct: number | null;
  /** 7D traded notional volume, USD. Pass null while loading. */
  sevenDVolume: number | null;
  /** 7D trade count. */
  sevenDTrades: number;
  /** 7D ROI as percent of starting collateral. Pass null if unknown. */
  sevenDRoiPct: number | null;
  /** Annualized projection of the 7D ROI, percent. Pass null if unknown. */
  sevenDRoiAnnualizedPct: number | null;
  /** Daily P&L values in display order (oldest → newest). */
  dailyPnls: number[];
  /** Matching labels per day, same length as dailyPnls. */
  dailyLabels: string[];
  className?: string;
  style?: CSSProperties;
}

const LABEL: CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.14em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};

const CAPTION: CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 10,
  color: "var(--fg-muted)",
};

const VALUE_BASE: CSSProperties = {
  fontFamily: "var(--font-display)",
  fontSize: 22,
  fontWeight: 400,
  letterSpacing: "-0.02em",
};

function MiniTile({
  label,
  value,
  sub,
  color,
}: {
  label: string;
  value: string;
  sub: string;
  color?: string;
}) {
  return (
    <div
      style={{
        flex: 1,
        minWidth: 140,
        padding: "14px 16px",
        background: "var(--bg-subtle)",
        border: "1px solid var(--border)",
        borderRadius: 12,
        display: "flex",
        flexDirection: "column",
        gap: 4,
      }}
    >
      <div style={LABEL}>{label}</div>
      <div style={{ ...VALUE_BASE, color: color ?? "var(--fg)" }}>{value}</div>
      <div style={CAPTION}>{sub}</div>
    </div>
  );
}

function fmtSignedUsd(n: number): string {
  const sign = n >= 0 ? "+" : "−";
  return `${sign}$${Math.abs(n).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

function fmtUsd(n: number): string {
  return `$${n.toLocaleString("en-US", { maximumFractionDigits: 0 })}`;
}

function fmtSignedPct(n: number): string {
  const sign = n >= 0 ? "+" : "";
  return `${sign}${n.toFixed(2)}%`;
}

const DASH = "—";

export function Performance({
  sevenDPnl,
  sevenDPnlVsPrevPct,
  sevenDVolume,
  sevenDTrades,
  sevenDRoiPct,
  sevenDRoiAnnualizedPct,
  dailyPnls,
  dailyLabels,
  className,
  style,
}: PerformanceProps) {
  const max = Math.max(...(dailyPnls.length > 0 ? dailyPnls.map((v) => Math.abs(v)) : [1]), 1);
  const pnlColor = sevenDPnl === null ? "var(--fg)" : (sevenDPnl >= 0 ? "var(--success)" : "var(--danger)");
  const roiColor = sevenDRoiPct === null ? "var(--fg)" : (sevenDRoiPct >= 0 ? "var(--success)" : "var(--danger)");
  const pnlDirection = sevenDPnlVsPrevPct === null ? "·" : (sevenDPnlVsPrevPct >= 0 ? "↑" : "↓");
  const pnlVsSub = sevenDPnlVsPrevPct === null
    ? `${DASH} vs prev wk`
    : `${pnlDirection} ${Math.abs(sevenDPnlVsPrevPct).toFixed(1)}% vs prev wk`;
  const roiSub = sevenDRoiAnnualizedPct === null
    ? `${DASH} annualized`
    : `annualized · ${Math.round(sevenDRoiAnnualizedPct)}%`;

  return (
    <div
      className={className}
      style={{ display: "flex", flexDirection: "column", gap: 12, width: "100%", ...style }}
    >
      <div style={{ display: "flex", gap: 10, flexWrap: "wrap" }}>
        <MiniTile
          label="7D P&L"
          value={sevenDPnl === null ? DASH : fmtSignedUsd(sevenDPnl)}
          sub={pnlVsSub}
          color={pnlColor}
        />
        <MiniTile
          label="7D Volume"
          value={sevenDVolume === null ? DASH : fmtUsd(sevenDVolume)}
          sub={`${sevenDTrades} trades`}
        />
        <MiniTile
          label="7D ROI"
          value={sevenDRoiPct === null ? DASH : fmtSignedPct(sevenDRoiPct)}
          sub={roiSub}
          color={roiColor}
        />
      </div>

      <div
        style={{
          padding: "16px 18px",
          background: "var(--bg-subtle)",
          border: "1px solid var(--border)",
          borderRadius: 12,
        }}
      >
        <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 14 }}>
          <div style={LABEL}>Daily P&amp;L</div>
          <div style={CAPTION}>last {dailyPnls.length} days</div>
        </div>
        <div
          style={{
            display: "grid",
            gridTemplateColumns: `repeat(${dailyPnls.length}, 1fr)`,
            gap: 8,
            alignItems: "end",
            height: 90,
          }}
        >
          {dailyPnls.map((v, i) => {
            const h = (Math.abs(v) / max) * 72;
            const pos = v >= 0;
            return (
              <div key={i} style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 4 }}>
                <div
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: 9,
                    color: pos ? "var(--success)" : "var(--danger)",
                    marginBottom: 2,
                  }}
                >
                  {pos ? "+" : "−"}${Math.abs(v).toFixed(0)}
                </div>
                <div
                  style={{
                    width: "100%",
                    maxWidth: 36,
                    height: h,
                    minHeight: 4,
                    background: pos ? "rgba(5,193,104,0.8)" : "rgba(255,90,101,0.8)",
                    borderRadius: 3,
                    boxShadow: pos
                      ? "0 0 12px rgba(5,193,104,0.2)"
                      : "0 0 12px rgba(255,90,101,0.2)",
                  }}
                />
                <div style={{ ...LABEL, fontSize: 9, letterSpacing: 0, textTransform: "none", color: "var(--fg-subtle)", marginTop: 2 }}>
                  {dailyLabels[i] ?? ""}
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

export default Performance;
