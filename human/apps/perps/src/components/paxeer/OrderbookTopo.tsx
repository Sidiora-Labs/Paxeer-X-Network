"use client";
import { useMemo, useState } from "react";

export function OrderbookTopo() {
  const COLS = 32, ROWS = 14;
  // generate a fake heatmap: bid side (left) + ask side (right) with noisy patches
  const grid = useMemo(() => {
    const g = [];
    for (let r = 0; r < ROWS; r++) {
      const row = [];
      for (let c = 0; c < COLS; c++) {
        const dist = Math.abs(c - COLS / 2);
        const wave = Math.sin((c + r * 0.4) * 0.6) + Math.cos(r * 0.5);
        const base = Math.max(0, 1 - dist / (COLS / 2.2));
        const v = base * (0.5 + wave * 0.25) + (Math.random() - 0.4) * 0.15;
        row.push(Math.max(0, Math.min(1, v)));
      }
      g.push(row);
    }
    return g;
  }, []);
  const [hover, setHover] = useState(null);

  const cellW = 14, cellH = 14, pad = 8;
  const W = COLS * cellW + pad * 2, H = ROWS * cellH + pad * 2;

  return (
    <div style={{ width: '100%', maxWidth: 560, padding: '16px 18px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 10 }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Book topography · PAX/USDC</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>30 min window</div>
      </div>
      <svg width="100%" viewBox={`0 0 ${W} ${H}`} style={{ display: 'block' }}>
        {grid.map((row, r) => row.map((v, c) => {
          const isBid = c < COLS / 2;
          const color = isBid ? `rgba(5,193,104,${v.toFixed(2)})` : `rgba(255,90,101,${v.toFixed(2)})`;
          return (
            <rect key={`${r}-${c}`}
              x={pad + c * cellW} y={pad + r * cellH} width={cellW - 1} height={cellH - 1}
              fill={color}
              onMouseEnter={() => setHover({ r, c, v })}
              onMouseLeave={() => setHover(null)}
              style={{ cursor: 'crosshair' }} />
          );
        }))}
        {/* mid line */}
        <line x1={pad + (COLS/2) * cellW - 0.5} y1={pad} x2={pad + (COLS/2) * cellW - 0.5} y2={H - pad}
          stroke="rgba(255,255,255,0.3)" strokeDasharray="2 3" />
        {/* axis labels */}
        <text x={pad + 4} y={H - 10} fill="#828282" fontFamily="JetBrains Mono" fontSize="9">0.478</text>
        <text x={pad + (COLS/2) * cellW} y={H - 10} fill="#8FA8FF" fontFamily="JetBrains Mono" fontSize="9" textAnchor="middle">0.4821</text>
        <text x={W - pad - 4} y={H - 10} fill="#828282" fontFamily="JetBrains Mono" fontSize="9" textAnchor="end">0.486</text>
      </svg>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 8, fontFamily: 'var(--font-mono)', fontSize: 10 }}>
        <span style={{ color: 'var(--success)' }}>BIDS</span>
        <span style={{ color: 'var(--fg-subtle)' }}>
          {hover ? `depth ${(hover.v * 100).toFixed(0)}% · ${Math.floor(hover.r * 2)}min ago` : 'hover for detail'}
        </span>
        <span style={{ color: 'var(--danger)' }}>ASKS</span>
      </div>
    </div>
  );
}

export default OrderbookTopo;
