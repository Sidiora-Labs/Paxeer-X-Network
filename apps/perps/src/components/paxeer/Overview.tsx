"use client";
import * as React from "react";
import { TOKENS, fmt } from "./_shared";

export function Overview() {
  const val = 24821.44, unreal = 182.40, unrealPct = 0.74, open = 4, realized = 1245.82;
  const card = {
    flex: 1, minWidth: 180, padding: '18px 20px',
    background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
    borderRadius: 14, display: 'flex', flexDirection: 'column', gap: 8,
  };
  const label = {
    fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em',
    textTransform: 'uppercase', color: 'var(--fg-subtle)',
  };
  const value = {
    fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
    fontSize: 28, fontWeight: 400, letterSpacing: '-0.02em', color: 'var(--fg)',
  };
  return (
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', width: '100%' }}>
      <div style={card}>
        <div style={label}>Total value</div>
        <div style={value}>${fmt(val, 2)}</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--success)' }}>▲ $421.88 (24h)</div>
      </div>
      <div style={card}>
        <div style={label}>Unrealized P&L</div>
        <div style={{ ...value, color: 'var(--success)' }}>+${fmt(unreal, 2)}</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>+{unrealPct.toFixed(2)}% · across {open}</div>
      </div>
      <div style={card}>
        <div style={label}>Open positions</div>
        <div style={value}>{open}</div>
        <div style={{ display: 'flex', gap: 4, marginTop: 2 }}>
          {['PAX','ETH','SOL','ARB'].map((s,i) => {
            const t = TOKENS.find(tk => tk.sym === s);
            return <div key={s} style={{ width: 18, height: 18, borderRadius: 999, background: t.color, border: '2px solid var(--bg-subtle)', marginLeft: i ? -6 : 0, display: 'grid', placeItems: 'center', color: '#fff', fontSize: 8, fontWeight: 600 }}>{s[0]}</div>;
          })}
        </div>
      </div>
      <div style={card}>
        <div style={label}>Realized P&L</div>
        <div style={{ ...value, color: 'var(--success)' }}>+${fmt(realized, 2)}</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>lifetime · 142 trades</div>
      </div>
    </div>
  );
}

export default Overview;
