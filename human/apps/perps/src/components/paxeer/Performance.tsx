"use client";
import * as React from "react";

export function Performance() {
  const days = ['Mon','Tue','Wed','Thu','Fri','Sat','Sun'];
  const pnls = [82.4, -34.2, 128.1, 56.8, -18.4, 92.2, 48.1];
  const max = Math.max(...pnls.map(Math.abs));
  const sparkPoints = (arr) => arr.map((v, i) => `${(i / (arr.length - 1)) * 60},${20 - ((v + max) / (max * 2)) * 18}`).join(' ');

  const miniTile = (lbl, v, sub, color) => (
    <div style={{
      flex: 1, minWidth: 140, padding: '14px 16px',
      background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 12,
      display: 'flex', flexDirection: 'column', gap: 4,
    }}>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>{lbl}</div>
      <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 22, fontWeight: 400, letterSpacing: '-0.02em', color: color || 'var(--fg)' }}>{v}</div>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>{sub}</div>
    </div>
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, width: '100%' }}>
      <div style={{ display: 'flex', gap: 10, flexWrap: 'wrap' }}>
        {miniTile('7D P&L', '+$355.00', '↑ 3.8% vs prev wk', 'var(--success)')}
        {miniTile('7D Volume', '$12,480', '24 trades', null)}
        {miniTile('7D ROI', '+1.42%', 'annualized · 74%', 'var(--success)')}
      </div>
      <div style={{
        padding: '16px 18px',
        background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 12,
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 14 }}>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Daily P&L</div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>last 7 days</div>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(7, 1fr)', gap: 8, alignItems: 'end', height: 90 }}>
          {pnls.map((v, i) => {
            const h = (Math.abs(v) / max) * 72;
            const pos = v >= 0;
            return (
              <div key={i} style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4 }}>
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, color: pos ? 'var(--success)' : 'var(--danger)', marginBottom: 2 }}>
                  {pos ? '+' : '−'}${Math.abs(v).toFixed(0)}
                </div>
                <div style={{
                  width: '100%', maxWidth: 36, height: h, minHeight: 4,
                  background: pos ? 'rgba(5,193,104,0.8)' : 'rgba(255,90,101,0.8)',
                  borderRadius: 3,
                  boxShadow: pos ? '0 0 12px rgba(5,193,104,0.2)' : '0 0 12px rgba(255,90,101,0.2)',
                }} />
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--fg-subtle)', marginTop: 2 }}>{days[i]}</div>
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

export default Performance;
