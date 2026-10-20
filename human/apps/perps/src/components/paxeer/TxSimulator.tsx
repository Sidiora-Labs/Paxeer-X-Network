"use client";
import { Fragment } from "react";

export function TxSimulator() {
  const before = [
    { sym: 'USDC', amt: 1500.00 },
    { sym: 'ETH',  amt: 0.4800 },
    { sym: 'PAX',  amt: 2592.40 },
  ];
  const after = [
    { sym: 'USDC', amt: 1000.00 },
    { sym: 'ETH',  amt: 0.4800 },
    { sym: 'PAX',  amt: 3629.96 },
  ];
  const risk = 18; // low

  return (
    <div style={{ width: 380, background: 'var(--bg-elevated)', border: '1px solid var(--border-strong)', borderRadius: 14, overflow: 'hidden', boxShadow: 'var(--shadow-06)' }}>
      <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-soft)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Simulation · safe to sign</div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 15, color: 'var(--fg)', marginTop: 2 }}>Swap 500 USDC → PAX</div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
          <div style={{ width: 8, height: 8, borderRadius: 999, background: 'var(--success)', boxShadow: '0 0 8px var(--success)' }} />
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--success)', letterSpacing: '0.06em' }}>RISK {risk}/100</span>
        </div>
      </div>
      <div style={{ padding: 14 }}>
        <div style={{ display: 'grid', gridTemplateColumns: '1fr 24px 1fr', gap: 8, fontFamily: 'var(--font-mono)', fontSize: 11 }}>
          <div style={{ fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Before</div>
          <div />
          <div style={{ fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>After</div>
          {before.map((b, i) => {
            const a = after[i];
            const diff = a.amt - b.amt;
            const color = diff > 0 ? 'var(--success)' : diff < 0 ? 'var(--danger)' : 'var(--fg-muted)';
            return (
              <Fragment key={b.sym}>
                <div style={{
                  padding: '8px 10px', background: 'var(--bg-subtle)', borderRadius: 6,
                  display: 'flex', justifyContent: 'space-between',
                  textDecoration: diff < 0 ? 'line-through' : 'none',
                  color: diff < 0 ? 'var(--fg-muted)' : 'var(--fg)',
                }}>
                  <span>{b.sym}</span>
                  <span>{b.amt.toFixed(b.sym === 'ETH' ? 4 : 2)}</span>
                </div>
                <div style={{ display: 'grid', placeItems: 'center', color }}>→</div>
                <div style={{
                  padding: '8px 10px', background: diff !== 0 ? `${color.replace('var(','').replace(')','')}` : 'var(--bg-subtle)',
                  background: diff > 0 ? 'rgba(5,193,104,0.12)' : diff < 0 ? 'rgba(255,90,101,0.12)' : 'var(--bg-subtle)',
                  borderRadius: 6, display: 'flex', justifyContent: 'space-between',
                  color: diff !== 0 ? color : 'var(--fg)',
                }}>
                  <span>{a.sym}</span>
                  <span>{a.amt.toFixed(b.sym === 'ETH' ? 4 : 2)}</span>
                </div>
              </Fragment>
            );
          })}
        </div>
      </div>
      <div style={{ padding: '10px 14px', borderTop: '1px solid var(--border-soft)', display: 'flex', flexDirection: 'column', gap: 5, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
        {[
          ['Route', 'Paxeer AMM v2', 'var(--fg)'],
          ['Price impact', '0.08%', 'var(--success)'],
          ['Gas (est)', '0.00042 PAX', 'var(--fg-muted)'],
          ['Slippage cap', '0.5%', 'var(--fg-muted)'],
        ].map(([k, v, c]) => (
          <div key={k} style={{ display: 'flex', justifyContent: 'space-between' }}>
            <span style={{ color: 'var(--fg-subtle)' }}>{k}</span>
            <span style={{ color: c }}>{v}</span>
          </div>
        ))}
      </div>
      <div style={{ display: 'flex', gap: 8, padding: 14, paddingTop: 4 }}>
        <button style={{ flex: 1, padding: 12, borderRadius: 999, background: 'transparent', border: '1px solid var(--border-soft)', color: 'var(--fg)', fontFamily: 'var(--font-body)', fontSize: 12, cursor: 'pointer' }}>Cancel</button>
        <button style={{ flex: 2, padding: 12, borderRadius: 999, background: 'var(--accent-solid)', color: '#fff', border: 'none', fontFamily: 'var(--font-body)', fontSize: 12, fontWeight: 500, cursor: 'pointer' }}>Sign &amp; Submit</button>
      </div>
    </div>
  );
}

export default TxSimulator;
