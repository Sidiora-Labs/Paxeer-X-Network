"use client";
import { useEffect, useState } from "react";
import { useAnimatedNumber } from "./_shared";

// and a live waveform. Click to cycle through data states.

export function LiquidStat() {
  const states = [
    { label: 'Portfolio', value: 12487.42, delta: 4.21, sym: '$', data: [32, 38, 34, 41, 45, 52, 48, 58, 62, 71, 68, 78, 84, 82, 92] },
    { label: '24h Volume', value: 8492104, delta: 12.84, sym: '$', data: [48, 52, 44, 56, 62, 58, 72, 68, 84, 82, 88, 96, 94, 102, 108] },
    { label: 'Liquidity', value: 204_592_180, delta: -2.14, sym: '$', data: [92, 88, 82, 86, 78, 72, 68, 64, 58, 52, 48, 44, 42, 38, 34] },
  ];
  const [idx, setIdx] = useState(0);
  const s = states[idx];
  const v = useAnimatedNumber(s.value, 900);

  // rolling waveform sampled over time
  const [phase, setPhase] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setPhase(p => p + 1), 80);
    return () => clearInterval(id);
  }, []);

  const fmtValue = (n) => {
    if (n >= 1_000_000) return (n/1_000_000).toFixed(2) + 'M';
    if (n >= 1_000) return n.toLocaleString('en-US',{maximumFractionDigits:2});
    return n.toFixed(2);
  };

  const W = 340, H = 80;
  const points = s.data.map((d, i) => {
    const x = (i / (s.data.length - 1)) * W;
    const shimmer = Math.sin((phase + i * 2) * 0.08) * 3;
    const y = H - ((d + shimmer) / 120) * H;
    return `${x},${y}`;
  }).join(' ');

  return (
    <div
      onClick={() => setIdx((idx + 1) % states.length)}
      style={{
        width: 360, padding: 24, borderRadius: 18,
        background: 'linear-gradient(165deg, #001E5C 0%, var(--brand-blue) 100%)',
        border: '1px solid rgba(143,168,255,0.25)',
        cursor: 'pointer', position: 'relative', overflow: 'hidden',
        boxShadow: '0 20px 40px -20px rgba(0,76,237,0.6)',
      }}>
      {/* top row */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 4 }}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.16em', textTransform: 'uppercase', color: 'rgba(255,255,255,0.7)' }}>
          {s.label} · live
        </span>
        <span style={{
          fontFamily: 'var(--font-mono)', fontSize: 11,
          padding: '3px 8px', borderRadius: 4,
          background: s.delta >= 0 ? 'rgba(5,193,104,0.18)' : 'rgba(255,90,101,0.18)',
          color: s.delta >= 0 ? '#7FDCA4' : '#FFBEC2',
          border: `1px solid ${s.delta >= 0 ? 'rgba(5,193,104,0.3)' : 'rgba(255,90,101,0.3)'}`,
        }}>
          {s.delta >= 0 ? '▲' : '▼'} {Math.abs(s.delta).toFixed(2)}%
        </span>
      </div>
      {/* big number */}
      <div style={{
        fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
        fontSize: 56, fontWeight: 400, letterSpacing: '-0.035em',
        color: '#fff', lineHeight: 1, marginTop: 10,
        fontVariantNumeric: 'tabular-nums',
      }}>
        <span style={{ fontSize: 32, color: 'rgba(255,255,255,0.6)', verticalAlign: 'top', marginRight: 2 }}>{s.sym}</span>
        {fmtValue(v)}
      </div>
      {/* waveform */}
      <svg width={W} height={H} style={{ marginTop: 14, display: 'block' }}>
        <defs>
          <linearGradient id="liquidFill" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="#fff" stopOpacity="0.4" />
            <stop offset="100%" stopColor="#fff" stopOpacity="0" />
          </linearGradient>
        </defs>
        <polyline points={`0,${H} ${points} ${W},${H}`} fill="url(#liquidFill)" />
        <polyline points={points} fill="none" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
      </svg>
      {/* hint */}
      <div style={{ marginTop: 10, fontFamily: 'var(--font-mono)', fontSize: 10, color: 'rgba(255,255,255,0.5)', letterSpacing: '0.1em', textTransform: 'uppercase' }}>
        tap to cycle · {idx + 1}/{states.length}
      </div>
    </div>
  );
}

export default LiquidStat;
