"use client";
import { useEffect, useState } from "react";
import { fmt } from "./_shared";

// New trades land with a subtle flash; aggressor side colors the row.

export function RecentTrades() {
  const [trades, setTrades] = useState(() => seedTrades());
  function seedTrades() {
    const base = 0.4821;
    return Array.from({ length: 12 }).map((_, i) => ({
      id: 'seed-' + i,
      price: base + (Math.random() - 0.5) * 0.001,
      size: 50 + Math.random() * 1200,
      side: Math.random() > 0.5 ? 'buy' : 'sell',
      t: new Date(Date.now() - i * 14000),
      fresh: false,
    }));
  }
  useEffect(() => {
    const id = setInterval(() => {
      setTrades(prev => {
        const next = {
          id: 'live-' + Date.now(),
          price: 0.4821 + (Math.random() - 0.5) * 0.001,
          size: 50 + Math.random() * 1200,
          side: Math.random() > 0.5 ? 'buy' : 'sell',
          t: new Date(),
          fresh: true,
        };
        return [next, ...prev.map(p => ({ ...p, fresh: false }))].slice(0, 12);
      });
    }, 2400);
    return () => clearInterval(id);
  }, []);

  const fmtT = (t) => t.toLocaleTimeString('en-US', { hour12: false });

  return (
    <div style={{ width: 340, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 12, overflow: 'hidden' }}>
      <div style={{
        display: 'grid', gridTemplateColumns: '1fr 1fr 1fr',
        padding: '10px 12px', borderBottom: '1px solid var(--border-soft)',
        fontFamily: 'var(--font-mono)', fontSize: 9,
        letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)',
      }}>
        <span>Price</span><span style={{ textAlign: 'right' }}>Size</span><span style={{ textAlign: 'right' }}>Time</span>
      </div>
      <div style={{ maxHeight: 320, overflow: 'hidden' }}>
        {trades.map((t) => {
          const color = t.side === 'buy' ? 'var(--success)' : 'var(--danger)';
          return (
            <div key={t.id}
              className={t.fresh ? 'gx-fade-in' : ''}
              style={{
                display: 'grid', gridTemplateColumns: '1fr 1fr 1fr',
                padding: '6px 12px',
                fontFamily: 'var(--font-mono)', fontSize: 12,
                background: t.fresh ? (t.side === 'buy' ? 'rgba(5,193,104,0.12)' : 'rgba(255,90,101,0.12)') : 'transparent',
                transition: 'background 1200ms',
              }}>
              <span style={{ color, display: 'inline-flex', alignItems: 'center', gap: 4, fontVariantNumeric: 'tabular-nums' }}>
                <span style={{ fontSize: 9 }}>{t.side === 'buy' ? '▲' : '▼'}</span>
                {t.price.toFixed(5)}
              </span>
              <span style={{ textAlign: 'right', color: 'var(--fg)', fontVariantNumeric: 'tabular-nums' }}>{fmt(t.size, 0)}</span>
              <span style={{ textAlign: 'right', color: 'var(--fg-subtle)' }}>{fmtT(t.t)}</span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export default RecentTrades;
