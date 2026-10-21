"use client";
import * as React from "react";
import { TOKENS } from "./_shared";

export function ClosedPositions() {
  const rows = [
    { sym: 'PAX', side: 'long',  size: 5000, entry: 0.4520, exit: 0.4821, pnl: 150.50, pct: 6.66, when: '2h ago' },
    { sym: 'ETH', side: 'short', size: 1.2,  entry: 3280,   exit: 3214,   pnl: 79.20,  pct: 2.01, when: '5h ago' },
    { sym: 'SOL', side: 'long',  size: 24,   entry: 142.10, exit: 138.42, pnl: -88.32, pct: -2.59, when: '1d ago' },
    { sym: 'BTC', side: 'long',  size: 0.02, entry: 66400,  exit: 67842,  pnl: 28.84,  pct: 2.17, when: '2d ago' },
    { sym: 'ARB', side: 'short', size: 500,  entry: 1.182,  exit: 1.142,  pnl: 20.00,  pct: 3.38, when: '3d ago' },
  ];
  return (
    <div style={{ width: '100%', maxWidth: 640, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-soft)', display: 'flex', justifyContent: 'space-between' }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Closed positions · {rows.length}</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--success)' }}>net +${rows.reduce((a,b)=>a+b.pnl,0).toFixed(2)}</div>
      </div>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
        <thead>
          <tr style={{ color: 'var(--fg-subtle)', fontSize: 9, letterSpacing: '0.1em' }}>
            <th style={{ textAlign: 'left', padding: '10px 18px 8px', fontWeight: 400 }}>MARKET</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>SIZE</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>ENTRY</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>EXIT</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>REALIZED</th>
            <th style={{ textAlign: 'right', padding: '10px 18px 8px', fontWeight: 400 }}>CLOSED</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => {
            const t = TOKENS.find(tk => tk.sym === r.sym);
            return (
              <tr key={i} style={{ borderTop: '1px solid rgba(255,255,255,0.04)', color: 'var(--fg)' }}>
                <td style={{ padding: '10px 18px' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <div style={{ width: 18, height: 18, borderRadius: 999, background: t.color, display: 'grid', placeItems: 'center', color: '#fff', fontSize: 9, fontWeight: 600 }}>{r.sym[0]}</div>
                    <span>{r.sym}</span>
                    <span style={{ padding: '1px 5px', borderRadius: 3, fontSize: 9, letterSpacing: '0.04em', textTransform: 'uppercase',
                      background: r.side === 'long' ? 'rgba(5,193,104,0.15)' : 'rgba(255,90,101,0.15)',
                      color: r.side === 'long' ? 'var(--success)' : 'var(--danger)' }}>{r.side}</span>
                  </div>
                </td>
                <td style={{ textAlign: 'right' }}>{r.size}</td>
                <td style={{ textAlign: 'right', color: 'var(--fg-muted)' }}>${r.entry.toLocaleString()}</td>
                <td style={{ textAlign: 'right' }}>${r.exit.toLocaleString()}</td>
                <td style={{ textAlign: 'right', color: r.pnl >= 0 ? 'var(--success)' : 'var(--danger)' }}>
                  {r.pnl >= 0 ? '+' : '−'}${Math.abs(r.pnl).toFixed(2)} <span style={{ fontSize: 9, opacity: 0.7 }}>({r.pct >= 0 ? '+' : ''}{r.pct.toFixed(2)}%)</span>
                </td>
                <td style={{ textAlign: 'right', padding: '10px 18px', color: 'var(--fg-subtle)' }}>{r.when}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export default ClosedPositions;
