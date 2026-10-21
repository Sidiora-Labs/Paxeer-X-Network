"use client";
import * as React from "react";

export function TradesTable() {
  const rows = [
    { when: '14:22:08', side: 'buy',  pair: 'PAX/USDC', px: 0.4821, amt: 1200, fee: 0.48 },
    { when: '14:18:42', side: 'sell', pair: 'ETH/USDC', px: 3214.88, amt: 0.28, fee: 0.90 },
    { when: '13:54:11', side: 'buy',  pair: 'SOL/USDC', px: 138.42, amt: 4.20, fee: 0.58 },
    { when: '13:22:00', side: 'buy',  pair: 'PAX/USDC', px: 0.4802, amt: 800, fee: 0.38 },
    { when: '12:48:31', side: 'sell', pair: 'BTC/USDC', px: 67842, amt: 0.004, fee: 0.27 },
    { when: '11:09:52', side: 'buy',  pair: 'ARB/USDC', px: 1.142, amt: 180, fee: 0.20 },
    { when: '10:31:04', side: 'sell', pair: 'OP/USDC', px: 2.384, amt: 48, fee: 0.23 },
  ];
  return (
    <div style={{ width: '100%', maxWidth: 560, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-soft)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Total trades</div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 22, fontWeight: 400, color: 'var(--fg)', marginTop: 2 }}>142 <span style={{ fontSize: 11, color: 'var(--fg-muted)' }}>lifetime</span></div>
        </div>
        <div style={{ display: 'flex', gap: 4 }}>
          {['ALL','BUYS','SELLS'].map(l => (
            <button key={l} style={{ background: l === 'ALL' ? 'var(--bg)' : 'transparent', border: '1px solid var(--border-soft)', color: l === 'ALL' ? 'var(--fg)' : 'var(--fg-muted)', padding: '4px 8px', borderRadius: 4, fontFamily: 'var(--font-mono)', fontSize: 9, cursor: 'pointer', letterSpacing: '0.06em' }}>{l}</button>
          ))}
        </div>
      </div>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
        <thead>
          <tr style={{ color: 'var(--fg-subtle)', fontSize: 9, letterSpacing: '0.1em' }}>
            <th style={{ textAlign: 'left', padding: '10px 18px 8px', fontWeight: 400 }}>TIME</th>
            <th style={{ textAlign: 'left', padding: '10px 0 8px', fontWeight: 400 }}>SIDE</th>
            <th style={{ textAlign: 'left', padding: '10px 0 8px', fontWeight: 400 }}>PAIR</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>PRICE</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>AMOUNT</th>
            <th style={{ textAlign: 'right', padding: '10px 18px 8px', fontWeight: 400 }}>FEE</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} style={{ borderTop: '1px solid rgba(255,255,255,0.04)', color: 'var(--fg)' }}>
              <td style={{ padding: '9px 18px', color: 'var(--fg-subtle)' }}>{r.when}</td>
              <td style={{ color: r.side === 'buy' ? 'var(--success)' : 'var(--danger)', textTransform: 'uppercase', fontSize: 10 }}>{r.side}</td>
              <td>{r.pair}</td>
              <td style={{ textAlign: 'right' }}>${r.px.toLocaleString()}</td>
              <td style={{ textAlign: 'right' }}>{r.amt}</td>
              <td style={{ textAlign: 'right', padding: '9px 18px', color: 'var(--fg-muted)' }}>${r.fee.toFixed(2)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export default TradesTable;
