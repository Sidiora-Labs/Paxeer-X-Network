"use client";
import { useState } from "react";
import { TOKENS, fmt } from "./_shared";

// Hover a segment to expand and reveal details. Center shows the hovered token's breakdown.

export function RadialAllocator() {
  const tokens = TOKENS.slice(0, 6);
  // compute values
  const enriched = tokens.map(t => ({ ...t, val: t.price * t.bal }));
  const total = enriched.reduce((s, t) => s + t.val, 0);
  const [hover, setHover] = useState(0);

  const R_OUT = 120, R_IN = 78, CX = 150, CY = 150;
  let cursor = -Math.PI / 2; // start at top

  const segments = enriched.map((t, i) => {
    const frac = t.val / total;
    const ang = frac * Math.PI * 2;
    const a0 = cursor, a1 = cursor + ang;
    cursor = a1;
    const isHover = i === hover;
    const r = isHover ? R_OUT + 10 : R_OUT;
    // arc path
    const large = ang > Math.PI ? 1 : 0;
    const p = (rad, a) => [CX + Math.cos(a) * rad, CY + Math.sin(a) * rad];
    const [x0, y0] = p(r, a0);
    const [x1, y1] = p(r, a1);
    const [x2, y2] = p(R_IN, a1);
    const [x3, y3] = p(R_IN, a0);
    const d = `M ${x0} ${y0} A ${r} ${r} 0 ${large} 1 ${x1} ${y1} L ${x2} ${y2} A ${R_IN} ${R_IN} 0 ${large} 0 ${x3} ${y3} Z`;
    return { t, d, frac, isHover };
  });

  const current = enriched[hover];
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 32 }}>
      <svg width="300" height="300" style={{ overflow: 'visible' }}>
        {segments.map((s, i) => (
          <path key={i} d={s.d}
            fill={s.t.color}
            opacity={s.isHover ? 1 : 0.55}
            stroke="var(--bg-elevated)" strokeWidth="2"
            onMouseEnter={() => setHover(i)}
            style={{ cursor: 'pointer', transition: 'opacity 200ms, d 300ms' }}
          />
        ))}
        {/* center */}
        <circle cx={CX} cy={CY} r={R_IN - 4} fill="var(--bg-elevated)" />
        <text x={CX} y={CY - 14} textAnchor="middle"
          fill="var(--fg-subtle)"
          style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em', textTransform: 'uppercase' }}>
          {current.sym} share
        </text>
        <text x={CX} y={CY + 14} textAnchor="middle"
          fill={current.color}
          style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 32, fontWeight: 500, letterSpacing: '-0.02em' }}>
          {(current.val / total * 100).toFixed(1)}%
        </text>
        <text x={CX} y={CY + 36} textAnchor="middle"
          fill="var(--fg-muted)"
          style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }}>
          ${fmt(current.val, 2)}
        </text>
      </svg>

      <div style={{ display: 'flex', flexDirection: 'column', gap: 4, width: 200 }}>
        {enriched.map((t, i) => (
          <div key={t.sym}
            onMouseEnter={() => setHover(i)}
            style={{
              display: 'grid', gridTemplateColumns: 'auto 1fr auto',
              alignItems: 'center', gap: 10,
              padding: '8px 10px', borderRadius: 8,
              background: i === hover ? 'var(--bg-subtle)' : 'transparent',
              cursor: 'pointer', transition: 'background 150ms',
            }}>
            <div style={{ width: 8, height: 8, borderRadius: 2, background: t.color }} />
            <div>
              <div style={{ fontSize: 13, fontWeight: 500 }}>{t.sym}</div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)' }}>{(t.val / total * 100).toFixed(1)}%</div>
            </div>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)' }}>
              ${fmt(t.val, 0)}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default RadialAllocator;
