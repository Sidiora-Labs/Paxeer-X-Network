"use client";
import * as React from "react";
import { TOKENS } from "./_shared";

export function TransfersHistory() {
  const rows = [
    { type: 'deposit',  sym: 'USDC', amt: 500.00, status: 'confirmed', hash: '0x7a3f…9b2c', when: '2h ago' },
    { type: 'withdraw', sym: 'PAX',  amt: 1200.00, status: 'pending',   hash: '0x8c2a…4f19', when: '12m ago' },
    { type: 'deposit',  sym: 'ETH',  amt: 0.25,   status: 'confirmed', hash: '0x5e11…78ab', when: '1d ago' },
    { type: 'withdraw', sym: 'USDC', amt: 300.00, status: 'confirmed', hash: '0x22d8…c031', when: '3d ago' },
    { type: 'deposit',  sym: 'SOL',  amt: 12.5,   status: 'confirmed', hash: '0xbb91…0a4e', when: '5d ago' },
  ];
  const statusColor = s => s === 'confirmed' ? 'var(--success)' : s === 'pending' ? 'var(--warning)' : 'var(--danger)';
  return (
    <div style={{ width: '100%', maxWidth: 600, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-soft)', display: 'flex', justifyContent: 'space-between' }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Deposits &amp; withdrawals</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>{rows.length} · last 30d</div>
      </div>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
        <thead>
          <tr style={{ color: 'var(--fg-subtle)', fontSize: 9, letterSpacing: '0.1em' }}>
            <th style={{ textAlign: 'left', padding: '10px 18px 8px', fontWeight: 400 }}>TYPE</th>
            <th style={{ textAlign: 'left', padding: '10px 0 8px', fontWeight: 400 }}>ASSET</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px', fontWeight: 400 }}>AMOUNT</th>
            <th style={{ textAlign: 'left', padding: '10px 16px 8px', fontWeight: 400 }}>STATUS</th>
            <th style={{ textAlign: 'left', padding: '10px 0 8px', fontWeight: 400 }}>HASH</th>
            <th style={{ textAlign: 'right', padding: '10px 18px 8px', fontWeight: 400 }}>WHEN</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => {
            const t = TOKENS.find(tk => tk.sym === r.sym);
            return (
              <tr key={i} style={{ borderTop: '1px solid rgba(255,255,255,0.04)', color: 'var(--fg)' }}>
                <td style={{ padding: '10px 18px' }}>
                  <div style={{ display: 'inline-flex', alignItems: 'center', gap: 6,
                    color: r.type === 'deposit' ? 'var(--success)' : 'var(--fg)' }}>
                    <span style={{ transform: r.type === 'deposit' ? 'rotate(180deg)' : 'none', display: 'inline-block' }}>↑</span>
                    <span style={{ textTransform: 'capitalize' }}>{r.type}</span>
                  </div>
                </td>
                <td>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <div style={{ width: 16, height: 16, borderRadius: 999, background: t.color, display: 'grid', placeItems: 'center', color: '#fff', fontSize: 8, fontWeight: 600 }}>{r.sym[0]}</div>
                    {r.sym}
                  </div>
                </td>
                <td style={{ textAlign: 'right' }}>{r.amt}</td>
                <td style={{ padding: '10px 16px' }}>
                  <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, color: statusColor(r.status), textTransform: 'uppercase', fontSize: 9, letterSpacing: '0.06em' }}>
                    <span style={{ width: 5, height: 5, borderRadius: 999, background: statusColor(r.status) }} />
                    {r.status}
                  </span>
                </td>
                <td style={{ color: 'var(--color-blue-200)', cursor: 'pointer' }}>{r.hash}</td>
                <td style={{ textAlign: 'right', padding: '10px 18px', color: 'var(--fg-subtle)' }}>{r.when}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

export default TransfersHistory;
