"use client";
import { useState } from "react";

export function GasWeather() {
  const [i, setI] = useState(1);
  const levels = [
    { gwei: 12,  label: 'Clear',   desc: 'cheap to ship',     cond: 'clear',  color: 'var(--brand-green)' },
    { gwei: 38,  label: 'Cloudy',  desc: 'normal conditions', cond: 'cloudy', color: '#8FA8FF' },
    { gwei: 124, label: 'Storm',   desc: 'surge pricing',     cond: 'storm',  color: 'var(--brand-orange)' },
    { gwei: 380, label: 'Blizzard',desc: 'extreme congestion', cond: 'blizzard', color: 'var(--brand-red)' },
  ];
  const cur = levels[i];

  // SVG icons drawn inline
  const WeatherIcon = ({ cond }) => {
    if (cond === 'clear') return (
      <svg width="80" height="80" viewBox="0 0 100 100">
        <circle cx="50" cy="50" r="22" fill={cur.color} />
        {Array.from({length:8}).map((_,k) => {
          const a = (k / 8) * Math.PI * 2;
          return <line key={k} x1={50 + Math.cos(a)*30} y1={50 + Math.sin(a)*30} x2={50 + Math.cos(a)*40} y2={50 + Math.sin(a)*40} stroke={cur.color} strokeWidth="3" strokeLinecap="round" />;
        })}
      </svg>
    );
    if (cond === 'cloudy') return (
      <svg width="80" height="80" viewBox="0 0 100 100">
        <path d="M25 65 Q15 65 15 55 Q15 45 25 45 Q25 30 45 30 Q65 30 65 45 Q80 45 80 58 Q80 70 70 70 L25 70 Z" fill={cur.color} opacity="0.7" />
      </svg>
    );
    if (cond === 'storm') return (
      <svg width="80" height="80" viewBox="0 0 100 100">
        <path d="M25 45 Q15 45 15 35 Q15 25 30 25 Q35 10 55 15 Q72 20 70 38 Q82 38 80 50 L25 50 Z" fill="#4a4a4a" />
        <path d="M48 52 L38 72 L48 72 L42 88 L58 66 L48 66 L54 52 Z" fill={cur.color} />
      </svg>
    );
    return (
      <svg width="80" height="80" viewBox="0 0 100 100">
        <path d="M25 40 Q15 40 15 30 Q15 20 30 20 Q35 5 55 10 Q72 15 70 33 Q82 33 80 45 L25 45 Z" fill="#2a2a2a" />
        {[[30,58],[45,62],[60,58],[70,64],[40,75],[58,78]].map(([x,y],k)=>
          <g key={k}><line x1={x-3} y1={y-3} x2={x+3} y2={y+3} stroke={cur.color} strokeWidth="2" strokeLinecap="round" /><line x1={x+3} y1={y-3} x2={x-3} y2={y+3} stroke={cur.color} strokeWidth="2" strokeLinecap="round" /></g>
        )}
      </svg>
    );
  };

  return (
    <div style={{ width: 280, padding: 18, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 10 }}>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Gas forecast</div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 14 }}>
        <WeatherIcon cond={cur.cond} />
        <div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 32, fontWeight: 300, color: cur.color, letterSpacing: '-0.02em' }}>{cur.gwei}</div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)', letterSpacing: '0.06em' }}>GWEI · {cur.label.toUpperCase()}</div>
        </div>
      </div>
      <div style={{ fontFamily: 'var(--font-body)', fontSize: 12, color: 'var(--fg)', fontStyle: 'italic' }}>{cur.desc}</div>
      <div style={{ display: 'flex', gap: 4, width: '100%', marginTop: 4 }}>
        {levels.map((l, k) => (
          <button key={k} onClick={() => setI(k)} style={{
            flex: 1, padding: '5px 0', background: i === k ? 'var(--bg)' : 'transparent',
            border: `1px solid ${i === k ? l.color : 'var(--border-soft)'}`,
            borderRadius: 6, color: i === k ? l.color : 'var(--fg-muted)',
            fontFamily: 'var(--font-mono)', fontSize: 9, cursor: 'pointer', letterSpacing: '0.04em',
          }}>{l.label}</button>
        ))}
      </div>
    </div>
  );
}

export default GasWeather;
