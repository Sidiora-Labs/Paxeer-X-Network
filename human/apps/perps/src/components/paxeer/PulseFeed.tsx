"use client";
import { useEffect, useState } from "react";
import { Icon } from "./_shared";

// Each row has a small oscillograph showing the transaction's gas/time signature.

export function PulseFeed() {
  const items = [
    { type: 'swap', from: 'USDC', to: 'PAX', amt: '500.00', val: '$1,036.92', status: 'confirmed', t: '2m ago', pulse: [20, 35, 50, 30, 60, 45, 40, 25, 30, 20] },
    { type: 'receive', from: 'ETH', amt: '+0.082', val: '+$263.62', status: 'confirmed', t: '14m ago', pulse: [10, 15, 25, 40, 65, 80, 60, 40, 25, 15] },
    { type: 'send', to: 'barbara.pax', amt: '-250 USDC', val: '-$250.00', status: 'pending', t: '24m ago', pulse: [22, 28, 35, 42, 38, 45, 52, 48, 54, 50] },
    { type: 'stake', asset: 'PAX', amt: '1,200.00', val: '$578.52', status: 'confirmed', t: '1h ago', pulse: [15, 18, 22, 28, 34, 40, 45, 50, 48, 52] },
  ];

  const colorFor = (status) => status === 'confirmed' ? 'var(--success)' : status === 'pending' ? 'var(--warning)' : 'var(--danger)';
  const iconFor = (type) => ({
    swap: <Icon.swap />, receive: <Icon.receive />, send: <Icon.send />, stake: <Icon.zap />,
  }[type]);

  const [phase, setPhase] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setPhase(p => p + 1), 80);
    return () => clearInterval(id);
  }, []);

  return (
    <div style={{ width: 420, display: 'flex', flexDirection: 'column' }}>
      {items.map((it, idx) => {
        const c = colorFor(it.status);
        const live = it.status === 'pending';
        const pts = it.pulse.map((v, i) => {
          const x = (i / (it.pulse.length - 1)) * 70;
          const shimmer = live ? Math.sin((phase + i * 2) * 0.12) * 5 : 0;
          const y = 24 - ((v + shimmer) / 90) * 24;
          return `${x},${y}`;
        }).join(' ');
        return (
          <div key={idx} style={{
            display: 'grid', gridTemplateColumns: '32px 1fr auto auto', gap: 14,
            alignItems: 'center', padding: '14px 0',
            borderBottom: idx < items.length - 1 ? '1px solid var(--border-soft)' : 'none',
          }}>
            <div style={{
              width: 32, height: 32, borderRadius: 10,
              background: 'var(--bg-subtle)',
              border: '1px solid var(--border-soft)',
              display: 'grid', placeItems: 'center', color: 'var(--fg-muted)',
            }}>
              {iconFor(it.type)}
            </div>
            <div>
              <div style={{ fontSize: 13, fontWeight: 500, textTransform: 'capitalize' }}>
                {it.type}{it.from && it.to ? ` ${it.from} → ${it.to}` : it.from ? ` ${it.from}` : it.to ? ` to ${it.to}` : it.asset ? ` ${it.asset}` : ''}
              </div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginTop: 3 }}>
                <span style={{
                  display: 'inline-flex', alignItems: 'center', gap: 5,
                  fontFamily: 'var(--font-mono)', fontSize: 10,
                  color: c, letterSpacing: '0.06em',
                }}>
                  <span style={{ width: 5, height: 5, borderRadius: 999, background: c, animation: live ? 'gxPulse 1.2s infinite' : 'none' }} />
                  {it.status}
                </span>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)' }}>· {it.t}</span>
              </div>
            </div>
            <svg width="70" height="24">
              <polyline points={pts} fill="none" stroke={c} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" opacity="0.85" />
            </svg>
            <div style={{ textAlign: 'right' }}>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{it.amt}</div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', marginTop: 2 }}>{it.val}</div>
            </div>
          </div>
        );
      })}
    </div>
  );
}

export default PulseFeed;
