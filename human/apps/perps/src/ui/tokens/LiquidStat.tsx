// LiquidStat — premium animated numeric stat card with morphing digits and live waveform.
// TS port of src/components/design/LiquidStat.jsx. "Logic matches prototype 1:1."
// Click to cycle through provided states.

import { useEffect, useState, type CSSProperties } from "react";
import { useAnimatedNumber } from "@/ui/hooks/useAnimatedNumber";

export interface LiquidStatState {
  label: string;
  value: number;
  delta: number;
  sym?: string;
  data: number[];
}

export interface LiquidStatProps {
  /** Cycle-able states for the card. Pass at least one real state — no mock fallback. */
  states: LiquidStatState[];
  width?: number;
  className?: string;
  style?: CSSProperties;
}

function fmtValue(n: number): string {
  if (n >= 1_000_000) return (n / 1_000_000).toFixed(2) + "M";
  if (n >= 1_000) return n.toLocaleString("en-US", { maximumFractionDigits: 2 });
  return n.toFixed(2);
}

export function LiquidStat({ states, width = 360, className, style }: LiquidStatProps) {
  if (states.length === 0) {
    throw new Error("LiquidStat: `states` must contain at least one entry.");
  }
  const [idx, setIdx] = useState(0);
  const s = states[idx] ?? states[0]!;
  const v = useAnimatedNumber(s.value, 900);

  const [phase, setPhase] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setPhase((p) => p + 1), 80);
    return () => clearInterval(id);
  }, []);

  const W = width - 20;
  const H = 80;
  const points = s.data
    .map((d, i) => {
      const x = (i / (s.data.length - 1)) * W;
      const shimmer = Math.sin((phase + i * 2) * 0.08) * 3;
      const y = H - ((d + shimmer) / 120) * H;
      return `${x},${y}`;
    })
    .join(" ");

  const sym = s.sym ?? "";
  const deltaUp = s.delta >= 0;

  return (
    <div
      onClick={() => setIdx((idx + 1) % states.length)}
      className={className}
      style={{
        width,
        padding: 24,
        borderRadius: 18,
        background: "linear-gradient(165deg, #001E5C 0%, var(--brand-blue) 100%)",
        border: "1px solid rgba(143,168,255,0.25)",
        cursor: "pointer",
        position: "relative",
        overflow: "hidden",
        boxShadow: "0 20px 40px -20px rgba(0,76,237,0.6)",
        ...style,
      }}
    >
      {/* top row */}
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 4 }}>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 10,
            letterSpacing: "0.16em",
            textTransform: "uppercase",
            color: "rgba(255,255,255,0.7)",
          }}
        >
          {s.label} · live
        </span>
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            padding: "3px 8px",
            borderRadius: 4,
            background: deltaUp ? "rgba(5,193,104,0.18)" : "rgba(255,90,101,0.18)",
            color: deltaUp ? "#7FDCA4" : "#FFBEC2",
            border: `1px solid ${deltaUp ? "rgba(5,193,104,0.3)" : "rgba(255,90,101,0.3)"}`,
          }}
        >
          {deltaUp ? "▲" : "▼"} {Math.abs(s.delta).toFixed(2)}%
        </span>
      </div>

      {/* big number */}
      <div
        style={{
          fontFamily: "var(--font-display)",
          fontSize: 56,
          fontWeight: 400,
          letterSpacing: "-0.035em",
          color: "#fff",
          lineHeight: 1,
          marginTop: 10,
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {sym && (
          <span style={{ fontSize: 32, color: "rgba(255,255,255,0.6)", verticalAlign: "top", marginRight: 2 }}>
            {sym}
          </span>
        )}
        {fmtValue(v)}
      </div>

      {/* waveform */}
      <svg width={W} height={H} style={{ marginTop: 14, display: "block" }}>
        <defs>
          <linearGradient id="liquidFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#fff" stopOpacity="0.4" />
            <stop offset="100%" stopColor="#fff" stopOpacity="0" />
          </linearGradient>
        </defs>
        <polyline points={`0,${H} ${points} ${W},${H}`} fill="url(#liquidFill)" />
        <polyline
          points={points}
          fill="none"
          stroke="#fff"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>

      {/* hint */}
      <div
        style={{
          marginTop: 10,
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          color: "rgba(255,255,255,0.5)",
          letterSpacing: "0.1em",
          textTransform: "uppercase",
        }}
      >
        tap to cycle · {idx + 1}/{states.length}
      </div>
    </div>
  );
}

export default LiquidStat;
