"use client";
import { useMemo, useState } from "react";

export function CumulativePnL() {
  const ranges = ['1D','7D','30D','ALL'];
  const [range, setRange] = useState('30D');
  const seed = { '1D': 24, '7D': 7, '30D': 30, 'ALL': 60 }[range];
  const series = useMemo(() => {
    const arr = [0];
    let v = 0;
    for (let i = 1; i < seed; i++) {
      v += (Math.sin(i * 0.5 + seed) + Math.cos(i * 0.3)) * 35 + (Math.random() - 0.3) * 25;
      arr.push(v);
    }
    return arr;
  }, [range]);
  const W = 520, H = 160, P = 8;
  const max = Math.max(...series), min = Math.min(...series);
  const rng = (max - min) || 1;
  const toX = i => P + (i / (series.length - 1)) * (W - P * 2);
  const toY = v => P + (1 - (v - min) / rng) * (H - P * 2);
  const line = series.map((v, i) => `${i ? 'L' : 'M'}${toX(i)} ${toY(v)}`).join(' ');
  const fill = `${line} L ${toX(series.length-1)} ${H-P} L ${toX(0)} ${H-P} Z`;
  const last = series[series.length - 1];
  const first = series[0];
  const pnl = last - first;
  const pct = (pnl / 10000) * 100;

  return (
    <div style={{ width: '100%', maxWidth: 560, padding: '18px 20px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 12 }}>
        <div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Cumulative P&L</div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 32, fontWeight: 400, letterSpacing: '-0.02em', color: pnl >= 0 ? 'var(--success)' : 'var(--danger)', marginTop: 4 }}>
            {pnl >= 0 ? '+' : '−'}${Math.abs(pnl).toFixed(2)}
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)', marginTop: 2 }}>
            {pct >= 0 ? '+' : ''}{pct.toFixed(2)}% · last {range}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 2, padding: 3, background: 'var(--bg)', border: '1px solid var(--border-soft)', borderRadius: 8 }}>
          {ranges.map(r => (
            <button key={r} onClick={() => setRange(r)} style={{
              padding: '5px 10px', border: 'none', borderRadius: 5,
              background: range === r ? 'var(--bg-subtle)' : 'transparent',
              color: range === r ? 'var(--fg)' : 'var(--fg-muted)',
              fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.04em', cursor: 'pointer',
            }}>{r}</button>
          ))}
        </div>
      </div>
      <svg width="100%" viewBox={`0 0 ${W} ${H}`} style={{ display: 'block' }}>
        <defs>
          <linearGradient id="pnlGrad" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={pnl >= 0 ? 'var(--brand-green)' : 'var(--brand-red)'} stopOpacity="0.35" />
            <stop offset="100%" stopColor={pnl >= 0 ? 'var(--brand-green)' : 'var(--brand-red)'} stopOpacity="0" />
          </linearGradient>
        </defs>
        {/* zero line */}
        <line x1={P} y1={toY(0)} x2={W - P} y2={toY(0)} stroke="rgba(255,255,255,0.08)" strokeDasharray="2 3" />
        <path d={fill} fill="url(#pnlGrad)" />
        <path d={line} fill="none" stroke={pnl >= 0 ? 'var(--brand-green)' : 'var(--brand-red)'} strokeWidth="1.75" strokeLinejoin="round" strokeLinecap="round" />
        <circle cx={toX(series.length-1)} cy={toY(last)} r="3.5" fill={pnl >= 0 ? 'var(--brand-green)' : 'var(--brand-red)'} stroke="var(--bg-subtle)" strokeWidth="2" />
      </svg>
    </div>
  );
}

export default CumulativePnL;
