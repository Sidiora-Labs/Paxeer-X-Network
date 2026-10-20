"use client";
import { useState } from "react";

export function RiskDial() {
  const factors = [
    { label: 'Leverage',      v: 42, weight: 0.35 },
    { label: 'Concentration', v: 68, weight: 0.25 },
    { label: 'Volatility',    v: 55, weight: 0.20 },
    { label: 'Liquidation',   v: 28, weight: 0.20 },
  ];
  const score = Math.round(factors.reduce((a, f) => a + f.v * f.weight, 0));
  const color = score >= 70 ? 'var(--danger)' : score >= 45 ? 'var(--warning)' : 'var(--success)';
  const label = score >= 70 ? 'HIGH' : score >= 45 ? 'ELEVATED' : 'LOW';

  const R = 88;
  const startA = Math.PI * 0.85, endA = Math.PI * 2.15;
  const valueA = startA + (score / 100) * (endA - startA);
  const arcP = (a1, a2) => {
    const large = (a2 - a1) > Math.PI ? 1 : 0;
    return `M ${Math.cos(a1)*R} ${Math.sin(a1)*R} A ${R} ${R} 0 ${large} 1 ${Math.cos(a2)*R} ${Math.sin(a2)*R}`;
  };

  const [hover, setHover] = useState(null);

  return (
    <div style={{ width: 360, padding: 20, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14, display: 'flex', gap: 18, alignItems: 'center' }}>
      <svg width="200" height="200" viewBox="-110 -110 220 220" style={{ flexShrink: 0 }}>
        <path d={arcP(startA, endA)} fill="none" stroke="rgba(255,255,255,0.05)" strokeWidth="10" strokeLinecap="round" />
        <path d={arcP(startA, valueA)} fill="none" stroke={color} strokeWidth="10" strokeLinecap="round" style={{ transition: 'all 400ms' }} />
        {/* tick marks */}
        {Array.from({length: 11}).map((_, i) => {
          const a = startA + (i/10) * (endA - startA);
          return <line key={i} x1={Math.cos(a)*(R+8)} y1={Math.sin(a)*(R+8)} x2={Math.cos(a)*(R+14)} y2={Math.sin(a)*(R+14)} stroke="rgba(255,255,255,0.2)" strokeWidth="1" />;
        })}
        <text x="0" y="0" textAnchor="middle" fill="var(--fg)" fontFamily="'Paxeer Grand Sans', Inter" fontSize="44" fontWeight="300" letterSpacing="-2">{score}</text>
        <text x="0" y="20" textAnchor="middle" fill={color} fontFamily="JetBrains Mono" fontSize="10" letterSpacing="3">{label}</text>
        <text x="0" y="55" textAnchor="middle" fill="var(--fg-subtle)" fontFamily="JetBrains Mono" fontSize="8" letterSpacing="2">RISK SCORE</text>
      </svg>
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 8 }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)', marginBottom: 2 }}>Contributing factors</div>
        {factors.map((f, i) => (
          <div key={f.label}
            onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}
            style={{ opacity: hover === null || hover === i ? 1 : 0.4, transition: 'opacity 180ms', cursor: 'pointer' }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 3, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
              <span style={{ color: 'var(--fg)' }}>{f.label}</span>
              <span style={{ color: 'var(--fg-muted)' }}>{f.v}</span>
            </div>
            <div style={{ height: 3, background: 'rgba(255,255,255,0.05)', borderRadius: 2, overflow: 'hidden' }}>
              <div style={{ width: `${f.v}%`, height: '100%', background: f.v >= 70 ? 'var(--danger)' : f.v >= 45 ? 'var(--warning)' : 'var(--success)' }} />
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default RiskDial;
