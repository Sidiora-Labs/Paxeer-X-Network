"use client";
import * as React from "react";

export function GhostHold() {
  const N = 60;
  const you = []; const hold = [];
  let vy = 0, vh = 0;
  for (let i = 0; i < N; i++) {
    vy += Math.sin(i * 0.3) * 20 + (Math.random() - 0.4) * 18;
    vh += Math.sin(i * 0.15 + 1) * 14 + 2.5;
    you.push(vy); hold.push(vh);
  }
  const all = [...you, ...hold];
  const max = Math.max(...all), min = Math.min(...all);
  const W = 520, H = 180, P = 10;
  const toX = i => P + (i / (N - 1)) * (W - P * 2);
  const toY = v => P + (1 - (v - min) / (max - min)) * (H - P * 2);
  const linePts = (a) => a.map((v, i) => `${toX(i)},${toY(v)}`).join(' ');

  const diff = you[N - 1] - hold[N - 1];
  const ahead = diff >= 0;

  return (
    <div style={{ width: '100%', maxWidth: 560, padding: '16px 18px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: 8 }}>
        <div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>You vs. just HODL</div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 22, fontWeight: 400, color: ahead ? 'var(--success)' : 'var(--danger)', marginTop: 2 }}>
            {ahead ? '+' : '−'}${Math.abs(diff).toFixed(2)} {ahead ? 'ahead' : 'behind'}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 12, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
          <span style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'var(--fg)' }}>
            <span style={{ width: 14, height: 2, background: 'var(--color-blue-200)' }} /> you
          </span>
          <span style={{ display: 'flex', alignItems: 'center', gap: 6, color: 'var(--fg-muted)' }}>
            <span style={{ width: 14, height: 2, background: 'var(--fg-subtle)', borderStyle: 'dashed' }} /> hodl ghost
          </span>
        </div>
      </div>
      <svg width="100%" viewBox={`0 0 ${W} ${H}`}>
        <line x1={P} y1={toY(0)} x2={W - P} y2={toY(0)} stroke="rgba(255,255,255,0.08)" strokeDasharray="2 3" />
        {/* ghost (dashed) */}
        <polyline points={linePts(hold)} fill="none" stroke="rgba(255,255,255,0.35)" strokeWidth="1.5" strokeDasharray="3 3" strokeLinejoin="round" />
        {/* you */}
        <polyline points={linePts(you)} fill="none" stroke={ahead ? 'var(--brand-green)' : 'var(--brand-red)'} strokeWidth="2" strokeLinejoin="round" strokeLinecap="round" />
        <circle cx={toX(N-1)} cy={toY(you[N-1])} r="3.5" fill={ahead ? 'var(--brand-green)' : 'var(--brand-red)'} stroke="var(--bg-subtle)" strokeWidth="2" />
        <circle cx={toX(N-1)} cy={toY(hold[N-1])} r="2.5" fill="rgba(255,255,255,0.4)" />
      </svg>
    </div>
  );
}

export default GhostHold;
