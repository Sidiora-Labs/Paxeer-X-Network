"use client";
import { useState } from "react";

export function FeesBreakdown() {
  const fees = [
    { label: 'Trading', value: 84.20, color: 'var(--brand-blue)' },
    { label: 'Gas',     value: 12.40, color: '#8FA8FF' },
    { label: 'Funding', value: 6.80,  color: 'var(--brand-orange)' },
    { label: 'Slippage', value: 3.22, color: 'var(--brand-green)' },
  ];
  const total = fees.reduce((a, b) => a + b.value, 0);
  const [hover, setHover] = useState(null);
  const R = 62, r = 42, cx = 80, cy = 80;
  let angle = -Math.PI / 2;
  const segs = fees.map((f, i) => {
    const sweep = (f.value / total) * Math.PI * 2;
    const a1 = angle, a2 = angle + sweep;
    angle = a2;
    const large = sweep > Math.PI ? 1 : 0;
    const x1 = cx + R * Math.cos(a1), y1 = cy + R * Math.sin(a1);
    const x2 = cx + R * Math.cos(a2), y2 = cy + R * Math.sin(a2);
    const x3 = cx + r * Math.cos(a2), y3 = cy + r * Math.sin(a2);
    const x4 = cx + r * Math.cos(a1), y4 = cy + r * Math.sin(a1);
    const d = `M ${x1} ${y1} A ${R} ${R} 0 ${large} 1 ${x2} ${y2} L ${x3} ${y3} A ${r} ${r} 0 ${large} 0 ${x4} ${y4} Z`;
    return { ...f, d, i };
  });
  const shown = hover !== null ? fees[hover] : null;

  return (
    <div style={{ width: 380, padding: '18px 20px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14, display: 'flex', gap: 16 }}>
      <div style={{ position: 'relative', width: 160, height: 160, flexShrink: 0 }}>
        <svg width="160" height="160">
          {segs.map(s => (
            <path key={s.i} d={s.d} fill={s.color}
              opacity={hover === null || hover === s.i ? 1 : 0.3}
              style={{ transition: 'opacity 180ms', cursor: 'pointer' }}
              onMouseEnter={() => setHover(s.i)} onMouseLeave={() => setHover(null)} />
          ))}
        </svg>
        <div style={{ position: 'absolute', inset: 0, display: 'grid', placeItems: 'center', pointerEvents: 'none' }}>
          <div style={{ textAlign: 'center' }}>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 8, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>{shown ? shown.label : 'Total fees'}</div>
            <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 20, fontWeight: 400, color: 'var(--fg)', marginTop: 2 }}>${(shown ? shown.value : total).toFixed(2)}</div>
            {shown && <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>{((shown.value/total)*100).toFixed(1)}%</div>}
          </div>
        </div>
      </div>
      <div style={{ flex: 1, display: 'flex', flexDirection: 'column', gap: 8, paddingTop: 4 }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)', marginBottom: 4 }}>Fees · 30d</div>
        {fees.map((f, i) => (
          <div key={f.label}
            onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)}
            style={{
              display: 'flex', alignItems: 'center', justifyContent: 'space-between',
              padding: '4px 0', cursor: 'pointer',
              opacity: hover === null || hover === i ? 1 : 0.4,
              transition: 'opacity 180ms',
            }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <div style={{ width: 8, height: 8, borderRadius: 2, background: f.color }} />
              <span style={{ fontFamily: 'var(--font-body)', fontSize: 12, color: 'var(--fg)' }}>{f.label}</span>
            </div>
            <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)' }}>${f.value.toFixed(2)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export default FeesBreakdown;
