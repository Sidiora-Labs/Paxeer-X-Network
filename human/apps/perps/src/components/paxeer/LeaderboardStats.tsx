"use client";
import * as React from "react";

export function LeaderboardStats() {
  const stats = [
    { label: 'Traders',       value: '2,481',    sub: '+184 this week', color: null },
    { label: 'Total P&L',     value: '$4.82M',   sub: '7d · +$321.4K',  color: 'var(--success)' },
    { label: 'Total trades',  value: '128,492',  sub: '~18K / day',     color: null },
    { label: 'Avg win rate',  value: '58.4%',    sub: 'top 10%: 74.2%', color: null },
  ];
  return (
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', width: '100%' }}>
      {stats.map((s, i) => (
        <div key={i} style={{
          flex: 1, minWidth: 160, padding: '16px 20px',
          background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
          borderRadius: 14, display: 'flex', flexDirection: 'column', gap: 6,
          position: 'relative', overflow: 'hidden',
        }}>
          {/* subtle rank-style numeral watermark */}
          <div style={{
            position: 'absolute', right: -10, top: -18,
            fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
            fontSize: 92, fontWeight: 300, letterSpacing: '-0.04em',
            color: 'rgba(255,255,255,0.02)', lineHeight: 1, pointerEvents: 'none',
          }}>0{i+1}</div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>{s.label}</div>
          <div style={{
            fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
            fontSize: 26, fontWeight: 400, letterSpacing: '-0.02em',
            color: s.color || 'var(--fg)',
          }}>{s.value}</div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>{s.sub}</div>
        </div>
      ))}
    </div>
  );
}

export default LeaderboardStats;
