"use client";
import * as React from "react";

export function ActiveOrders() {
  const rows = [
    { t: 'limit', side: 'buy',  pair: 'PAX/USDC', px: 0.4700, amt: 1000, filled: 12, age: '4m' },
    { t: 'limit', side: 'sell', pair: 'ETH/USDC', px: 3350.0, amt: 0.5,  filled: 0,  age: '1h' },
    { t: 'stop',  side: 'sell', pair: 'SOL/USDC', px: 128.00, amt: 10,   filled: 0,  age: '3h' },
    { t: 'limit', side: 'buy',  pair: 'BTC/USDC', px: 65000,  amt: 0.01, filled: 42, age: '12h' },
  ];
  return (
    <div style={{ width: '100%', maxWidth: 560, padding: '16px 0 8px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', padding: '0 18px 12px', borderBottom: '1px solid var(--border-soft)' }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Active orders · {rows.length}</div>
        <button style={{ background: 'none', border: 'none', color: 'var(--danger)', fontFamily: 'var(--font-mono)', fontSize: 10, cursor: 'pointer', letterSpacing: '0.04em' }}>CANCEL ALL</button>
      </div>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
        <thead>
          <tr style={{ color: 'var(--fg-subtle)', fontSize: 9, letterSpacing: '0.1em' }}>
            <th style={{ textAlign: 'left', padding: '10px 18px 8px', fontWeight: 400 }}>TYPE</th>
            <th style={{ textAlign: 'left', padding: '10px 0 8px', fontWeight: 400 }}>PAIR</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>PRICE</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>AMOUNT</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>FILLED</th>
            <th style={{ textAlign: 'right', padding: '10px 18px 8px', fontWeight: 400 }}></th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} style={{ borderTop: '1px solid rgba(255,255,255,0.04)', color: 'var(--fg)' }}>
              <td style={{ padding: '10px 18px' }}>
                <span style={{
                  display: 'inline-block', padding: '2px 6px', borderRadius: 3,
                  background: r.side === 'buy' ? 'rgba(5,193,104,0.15)' : 'rgba(255,90,101,0.15)',
                  color: r.side === 'buy' ? 'var(--success)' : 'var(--danger)',
                  fontSize: 9, letterSpacing: '0.06em', textTransform: 'uppercase',
                }}>{r.side} {r.t}</span>
              </td>
              <td>{r.pair}</td>
              <td style={{ textAlign: 'right' }}>${r.px.toLocaleString()}</td>
              <td style={{ textAlign: 'right' }}>{r.amt}</td>
              <td style={{ textAlign: 'right', color: r.filled ? 'var(--warning)' : 'var(--fg-muted)' }}>{r.filled}%</td>
              <td style={{ textAlign: 'right', padding: '10px 18px' }}>
                <button style={{ background: 'var(--bg)', border: '1px solid var(--border-soft)', color: 'var(--fg-muted)', padding: '3px 8px', borderRadius: 4, fontSize: 9, cursor: 'pointer', letterSpacing: '0.04em' }}>CANCEL</button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default ActiveOrders;
