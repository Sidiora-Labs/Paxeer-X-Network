"use client";
import { useEffect, useState } from "react";

// Instead of "Pending/Confirmed", show block-by-block confirmation with filling segments.

export function ShardStatus() {
  const [blocks, setBlocks] = useState(0);
  const TOTAL = 12;

  useEffect(() => {
    if (blocks >= TOTAL) return;
    const id = setTimeout(() => setBlocks(b => b + 1), 500);
    return () => clearTimeout(id);
  }, [blocks]);

  const state = blocks === 0 ? 'submitted' : blocks < TOTAL ? 'confirming' : 'finalized';
  const color = state === 'finalized' ? 'var(--success)' : 'var(--color-blue-200)';
  const bg = state === 'finalized' ? 'rgba(5,193,104,0.12)' : 'rgba(0,76,237,0.14)';
  const border = state === 'finalized' ? 'rgba(5,193,104,0.35)' : 'rgba(0,76,237,0.35)';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, alignItems: 'center' }}>
      <div style={{
        display: 'inline-flex', alignItems: 'center', gap: 12,
        padding: '10px 16px 10px 14px',
        borderRadius: 999,
        background: bg,
        border: `1px solid ${border}`,
        fontFamily: 'var(--font-mono)',
        fontSize: 12, color: color, letterSpacing: '0.04em',
      }}>
        {/* segments */}
        <div style={{ display: 'flex', gap: 2 }}>
          {Array.from({ length: TOTAL }).map((_, i) => {
            const filled = i < blocks;
            return (
              <div key={i} style={{
                width: 4, height: 14, borderRadius: 1,
                background: filled ? color : 'rgba(255,255,255,0.1)',
                transition: 'background 300ms cubic-bezier(0.4,0,0.2,1)',
                boxShadow: filled && i === blocks - 1 ? `0 0 8px ${color}` : 'none',
              }} />
            );
          })}
        </div>
        <span style={{ textTransform: 'capitalize' }}>{state}</span>
        <span style={{ color: 'var(--fg-subtle)' }}>·</span>
        <span style={{ fontVariantNumeric: 'tabular-nums' }}>{blocks}/{TOTAL}</span>
      </div>

      {/* size variations */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8, width: 320 }}>
        {[
          { n: 4, total: 12, label: 'Ethereum', sub: 'confirming', c: '#8FA8FF' },
          { n: 12, total: 12, label: 'Paxeer', sub: 'finalized', c: '#7FDCA4' },
          { n: 1, total: 6, label: 'Solana', sub: 'voting', c: '#FFD19B' },
        ].map((r, i) => (
          <div key={i} style={{
            display: 'grid', gridTemplateColumns: '1fr auto', alignItems: 'center', gap: 12,
            padding: '10px 14px', background: 'var(--bg-subtle)',
            border: '1px solid var(--border-soft)', borderRadius: 12,
          }}>
            <div>
              <div style={{ fontSize: 13, fontWeight: 500 }}>{r.label}</div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: r.c, letterSpacing: '0.08em', textTransform: 'uppercase', marginTop: 2 }}>
                {r.sub} · {r.n}/{r.total}
              </div>
            </div>
            <div style={{ display: 'flex', gap: 2 }}>
              {Array.from({ length: r.total }).map((_, j) => (
                <div key={j} style={{
                  width: 3, height: 16, borderRadius: 1,
                  background: j < r.n ? r.c : 'rgba(255,255,255,0.08)',
                }} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default ShardStatus;
