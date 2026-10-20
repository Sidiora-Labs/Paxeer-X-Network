"use client";
import { useState } from "react";
import { Icon } from "./_shared";

// Shows latency as animated bars and chain as a morphing pill.

export function NetworkPill() {
  const nets = [
    { id: 'paxeer', name: 'Paxeer', chain: 125, color: 'var(--brand-blue)', latency: 42 },
    { id: 'eth',    name: 'Ethereum', chain: 1, color: '#627EEA', latency: 184 },
    { id: 'arb',    name: 'Arbitrum', chain: 42161, color: '#28A0F0', latency: 98 },
    { id: 'op',     name: 'Optimism', chain: 10, color: '#FF0420', latency: 112 },
  ];
  const [idx, setIdx] = useState(0);
  const [open, setOpen] = useState(false);
  const n = nets[idx];

  // latency → signal bars
  const bars = n.latency < 60 ? 4 : n.latency < 100 ? 3 : n.latency < 150 ? 2 : 1;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, alignItems: 'flex-start', width: 260 }}>
      <button
        onClick={() => setOpen(o => !o)}
        style={{
          display: 'inline-flex', alignItems: 'center', gap: 10,
          background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
          borderRadius: 999, padding: '8px 14px 8px 10px',
          color: '#fff', fontFamily: 'var(--font-body)', fontSize: 13, fontWeight: 500,
          cursor: 'pointer',
        }}>
        <span style={{ width: 20, height: 20, borderRadius: 999, background: n.color, flexShrink: 0 }} />
        <span>{n.name}</span>
        <span style={{ display: 'flex', alignItems: 'flex-end', gap: 1.5, height: 12 }}>
          {[4, 7, 10, 12].map((h, i) => (
            <span key={i} style={{
              width: 2, height: h, borderRadius: 1,
              background: i < bars ? 'var(--success)' : 'rgba(255,255,255,0.12)',
            }} />
          ))}
        </span>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', paddingLeft: 4, borderLeft: '1px solid var(--border-soft)' }}>
          {n.latency}ms
        </span>
        <Icon.chevron style={{ transform: open ? 'rotate(180deg)' : 'none', transition: 'transform 200ms', color: 'var(--fg-muted)' }} />
      </button>

      {open && (
        <div className="gx-fade-in" style={{
          width: 260, padding: 6,
          background: 'var(--bg-elevated)', border: '1px solid var(--border-strong)',
          borderRadius: 12, boxShadow: 'var(--shadow-05)',
        }}>
          {nets.map((net, i) => {
            const b = net.latency < 60 ? 4 : net.latency < 100 ? 3 : net.latency < 150 ? 2 : 1;
            return (
              <button key={net.id}
                onClick={() => { setIdx(i); setOpen(false); }}
                style={{
                  display: 'grid', gridTemplateColumns: '20px 1fr auto auto', gap: 10,
                  alignItems: 'center', width: '100%',
                  padding: '8px 10px', borderRadius: 8,
                  background: i === idx ? 'var(--bg-subtle)' : 'transparent',
                  border: 'none', color: '#fff', cursor: 'pointer',
                  fontFamily: 'var(--font-body)', fontSize: 13,
                  textAlign: 'left',
                }}>
                <span style={{ width: 16, height: 16, borderRadius: 999, background: net.color }} />
                <div>
                  <div style={{ fontSize: 13, fontWeight: 500 }}>{net.name}</div>
                  <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)' }}>chain {net.chain}</div>
                </div>
                <span style={{ display: 'flex', alignItems: 'flex-end', gap: 1.5, height: 12 }}>
                  {[4, 7, 10, 12].map((h, j) => (
                    <span key={j} style={{ width: 2, height: h, borderRadius: 1, background: j < b ? 'var(--success)' : 'rgba(255,255,255,0.12)' }} />
                  ))}
                </span>
                <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)' }}>{net.latency}ms</span>
              </button>
            );
          })}
        </div>
      )}
    </div>
  );
}

export default NetworkPill;
