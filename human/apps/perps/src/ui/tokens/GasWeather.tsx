"use client";
import { activePoll } from "@/core/net/realtime";

// GasWeather — gas price expressed as a weather condition.
// TS port of src/components/design/GasWeather.jsx, rewired to real chain data
// via `viem.getGasPrice()`. No mock data: if the RPC call fails, renders a
// muted "offline" state. Thresholds are generic EVM gwei bands — Paxeer
// nominally runs below "Clear" so that zone is the common render.

import { useEffect, useRef, useState, type CSSProperties } from "react";
import { usePublicClient } from "wagmi";

const POLL_MS = 15_000;

type Condition = "clear" | "cloudy" | "storm" | "blizzard";

interface Zone {
  max: number; // gwei, upper bound (exclusive)
  label: string;
  desc: string;
  cond: Condition;
  color: string;
}

// Tuned to EVM mainnet semantics; still honest for low-gas chains like Paxeer
// which will consistently sit in "Clear".
const ZONES: readonly Zone[] = [
  { max: 30,  label: "Clear",    desc: "cheap to ship",      cond: "clear",    color: "var(--success)" },
  { max: 80,  label: "Cloudy",   desc: "normal conditions",  cond: "cloudy",   color: "#8FA8FF" },
  { max: 200, label: "Storm",    desc: "surge pricing",      cond: "storm",    color: "var(--warning)" },
  { max: Infinity, label: "Blizzard", desc: "extreme congestion", cond: "blizzard", color: "var(--danger)" },
];

function zoneFor(gwei: number): Zone {
  for (const z of ZONES) if (gwei < z.max) return z;
  return ZONES[ZONES.length - 1]!;
}

function useGasPrice(): { wei: bigint | null; gwei: number | null; offline: boolean } {
  const client = usePublicClient();
  const [wei, setWei] = useState<bigint | null>(null);
  const [offline, setOffline] = useState(false);
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    if (!client) return () => { mountedRef.current = false; };
    const poll = async () => {
      try {
        const raw = await client.getGasPrice();
        if (!mountedRef.current) return;
        setWei(raw);
        setOffline(false);
      } catch {
        if (!mountedRef.current) return;
        setOffline(true);
      }
    };
    const stopPoll = activePoll(poll, POLL_MS);
    return () => {
      mountedRef.current = false;
      stopPoll();
    };
  }, [client]);

  const gwei = wei === null ? null : Number(wei) / 1e9;
  return { wei, gwei, offline };
}

interface GasDisplay {
  readonly value: string;
  readonly unit: "gwei" | "wei" | "—";
}

/** Paxeer-safe gas formatting — switches to wei when sub-gwei. */
function formatGas(wei: bigint | null, gwei: number | null): GasDisplay {
  if (wei === null || gwei === null) return { value: "—", unit: "—" };
  if (gwei >= 1) {
    return {
      value: gwei >= 10 ? Math.round(gwei).toString() : gwei.toFixed(2),
      unit: "gwei",
    };
  }
  // Sub-gwei: show raw wei (no fractional unit confusion).
  // bigint → Number is safe: any realistic gas price fits in a Number.
  return { value: wei.toString(), unit: "wei" };
}

function WeatherIcon({ cond, color }: { cond: Condition; color: string }) {
  if (cond === "clear") {
    return (
      <svg width="80" height="80" viewBox="0 0 100 100" aria-hidden>
        <circle cx="50" cy="50" r="22" fill={color} />
        {Array.from({ length: 8 }).map((_, k) => {
          const a = (k / 8) * Math.PI * 2;
          return (
            <line
              key={k}
              x1={50 + Math.cos(a) * 30}
              y1={50 + Math.sin(a) * 30}
              x2={50 + Math.cos(a) * 40}
              y2={50 + Math.sin(a) * 40}
              stroke={color}
              strokeWidth="3"
              strokeLinecap="round"
            />
          );
        })}
      </svg>
    );
  }
  if (cond === "cloudy") {
    return (
      <svg width="80" height="80" viewBox="0 0 100 100" aria-hidden>
        <path
          d="M25 65 Q15 65 15 55 Q15 45 25 45 Q25 30 45 30 Q65 30 65 45 Q80 45 80 58 Q80 70 70 70 L25 70 Z"
          fill={color}
          opacity="0.7"
        />
      </svg>
    );
  }
  if (cond === "storm") {
    return (
      <svg width="80" height="80" viewBox="0 0 100 100" aria-hidden>
        <path d="M25 45 Q15 45 15 35 Q15 25 30 25 Q35 10 55 15 Q72 20 70 38 Q82 38 80 50 L25 50 Z" fill="#4a4a4a" />
        <path d="M48 52 L38 72 L48 72 L42 88 L58 66 L48 66 L54 52 Z" fill={color} />
      </svg>
    );
  }
  // blizzard
  return (
    <svg width="80" height="80" viewBox="0 0 100 100" aria-hidden>
      <path d="M25 40 Q15 40 15 30 Q15 20 30 20 Q35 5 55 10 Q72 15 70 33 Q82 33 80 45 L25 45 Z" fill="#2a2a2a" />
      {([[30, 58], [45, 62], [60, 58], [70, 64], [40, 75], [58, 78]] as const).map(([x, y], k) => (
        <g key={k}>
          <line x1={x - 3} y1={y - 3} x2={x + 3} y2={y + 3} stroke={color} strokeWidth="2" strokeLinecap="round" />
          <line x1={x + 3} y1={y - 3} x2={x - 3} y2={y + 3} stroke={color} strokeWidth="2" strokeLinecap="round" />
        </g>
      ))}
    </svg>
  );
}

export interface GasWeatherProps {
  className?: string;
  style?: CSSProperties;
}

const WRAPPER: CSSProperties = {
  width: 280,
  padding: 18,
  background: "var(--bg-subtle)",
  border: "1px solid var(--border)",
  borderRadius: 14,
  display: "flex",
  flexDirection: "column",
  alignItems: "center",
  gap: 10,
};

const LABEL_MONO: CSSProperties = {
  fontFamily: "var(--font-mono)",
  fontSize: 9,
  letterSpacing: "0.14em",
  textTransform: "uppercase",
  color: "var(--fg-subtle)",
};

export function GasWeather({ className, style }: GasWeatherProps) {
  const { wei, gwei, offline } = useGasPrice();
  const known = gwei !== null;
  const current = known ? zoneFor(gwei) : null;
  const displayColor = current?.color ?? "var(--fg-muted)";
  const displayCond: Condition = current?.cond ?? "cloudy";
  const gas = formatGas(wei, gwei);
  const displayLabel = offline
    ? "OFFLINE"
    : current
      ? current.label.toUpperCase()
      : "LOADING";
  const displayDesc = offline
    ? "rpc unreachable"
    : current?.desc ?? "measuring latency";

  return (
    <div className={className} style={{ ...WRAPPER, ...style }}>
      <div style={LABEL_MONO}>Gas forecast</div>
      <div style={{ display: "flex", alignItems: "center", gap: 14 }}>
        <WeatherIcon cond={displayCond} color={displayColor} />
        <div>
          <div
            style={{
              fontFamily: "var(--font-display)",
              fontSize: 32,
              fontWeight: 300,
              color: displayColor,
              letterSpacing: "-0.02em",
              fontVariantNumeric: "tabular-nums",
            }}
          >
            {gas.value}
          </div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 10,
              color: "var(--fg-muted)",
              letterSpacing: "0.06em",
            }}
          >
            {gas.unit.toUpperCase()} · {displayLabel}
          </div>
        </div>
      </div>
      <div
        style={{
          fontFamily: "var(--font-body)",
          fontSize: 12,
          color: "var(--fg)",
          fontStyle: "italic",
        }}
      >
        {displayDesc}
      </div>
      {/* Zone indicator — highlights the current zone, not a picker. */}
      <div style={{ display: "flex", gap: 4, width: "100%", marginTop: 4 }}>
        {ZONES.map((z) => {
          const active = known && current?.cond === z.cond;
          return (
            <div
              key={z.cond}
              style={{
                flex: 1,
                padding: "5px 0",
                textAlign: "center",
                background: active ? "var(--bg)" : "transparent",
                border: `1px solid ${active ? z.color : "var(--border)"}`,
                borderRadius: 6,
                color: active ? z.color : "var(--fg-muted)",
                fontFamily: "var(--font-mono)",
                fontSize: 9,
                letterSpacing: "0.04em",
              }}
            >
              {z.label}
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default GasWeather;
