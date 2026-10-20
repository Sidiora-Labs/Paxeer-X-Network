"use client";
import { useEffect, useState } from "react";
import { fmt } from "./_shared";

// Rows animate on updates; hover a row to preview fill.

export function OrderBook() {
  const MID = 0.4821;
  const [tick, setTick] = useState(0);
  useEffect(() => {
    const id = setInterval(() => setTick(t => t + 1), 1800);
    return () => clearInterval(id);
  }, []);

  // deterministic pseudo-random variation per tick
  const seed = (i, t, salt = 0) => {
    const x = Math.sin(i * 12.9898 + t * 78.233 + salt * 37.719) * 43758.5453;
    return x - Math.floor(x);
  };

  const asks = Array.from({ length: 8 }).map((_, i) => {
    const price = MID + (8 - i) * 0.00015 + seed(i, tick, 1) * 0.00008;
    const sz = 200 + seed(i, tick, 2) * 3200;
    return { price, sz, depth: sz };
  });
  const bids = Array.from({ length: 8 }).map((_, i) => {
    const price = MID - (i + 1) * 0.00015 - seed(i, tick, 3) * 0.00008;
    const sz = 200 + seed(i, tick, 4) * 3200;
    return { price, sz, depth: sz };
  });
  const maxDepth = Math.max(...asks.map(r => r.depth), ...bids.map(r => r.depth));

  const Row = ({ r, kind }) => {
    const color = kind === 'ask' ? 'var(--danger)' : 'var(--success)';
    const bg = kind === 'ask' ? 'rgba(255,90,101,0.08)' : 'rgba(5,193,104,0.08)';
    const pct = (r.depth / maxDepth) * 100;
    return (
      <div style={{
        display: 'grid', gridTemplateColumns: '1fr 1fr 1fr',
        padding: '5px 12px', fontFamily: 'var(--font-mono)', fontSize: 12,
        position: 'relative', cursor: 'pointer',
      }}>
        <div style={{
          position: 'absolute', top: 0, bottom: 0, right: 0,
          width: `${pct}%`, background: bg,
          transition: 'width 600ms cubic-bezier(0.4,0,0.2,1)',
        }} />
        <span style={{ color, position: 'relative', fontVariantNumeric: 'tabular-nums' }}>{r.price.toFixed(5)}</span>
        <span style={{ textAlign: 'right', color: 'var(--fg)', position: 'relative', fontVariantNumeric: 'tabular-nums' }}>{fmt(r.sz, 0)}</span>
        <span style={{ textAlign: 'right', color: 'var(--fg-muted)', position: 'relative', fontVariantNumeric: 'tabular-nums' }}>{fmt(r.sz * r.price, 2)}</span>
      </div>
    );
  };

  return (
    <div style={{ width: 340, display: 'flex', flexDirection: 'column', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 12, overflow: 'hidden' }}>
      {/* header */}
      <div style={{
        display: 'grid', gridTemplateColumns: '1fr 1fr 1fr',
        padding: '10px 12px', borderBottom: '1px solid var(--border-soft)',
        fontFamily: 'var(--font-mono)', fontSize: 9,
        letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)',
      }}>
        <span>Price</span>
        <span style={{ textAlign: 'right' }}>Size (PAX)</span>
        <span style={{ textAlign: 'right' }}>Total (USDC)</span>
      </div>

      {/* asks (highest at top → lowest near mid) */}
      <div>{asks.map((r, i) => <Row key={i} r={r} kind="ask" />)}</div>

      {/* mid marker */}
      <div style={{
        display: 'flex', alignItems: 'center', justifyContent: 'space-between',
        padding: '8px 12px',
        background: 'linear-gradient(90deg, rgba(0,76,237,0.12), transparent)',
        borderTop: '1px solid var(--border-soft)',
        borderBottom: '1px solid var(--border-soft)',
      }}>
        <div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 18, fontWeight: 500, color: 'var(--color-blue-200)' }}>
            ${MID.toFixed(5)}
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--fg-subtle)', letterSpacing: '0.1em', textTransform: 'uppercase', marginTop: 1 }}>
            mid · spread 0.00030
          </div>
        </div>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--success)' }}>▲ 4.21%</span>
      </div>

      {/* bids */}
      <div>{bids.map((r, i) => <Row key={i} r={r} kind="bid" />)}</div>
    </div>
  );
}

export default OrderBook;
