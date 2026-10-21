"use client";
import { useState } from "react";

export function TradePathSankey() {
  // flows: from token → to token, USD value
  const nodes = [
    { id: 'USDC', x: 0, y: 0,  h: 160, color: '#2775CA' },
    { id: 'ETH',  x: 1, y: 0,  h: 100, color: '#627EEA' },
    { id: 'PAX',  x: 1, y: 110, h: 80, color: 'var(--brand-blue)' },
    { id: 'SOL',  x: 2, y: 0,  h: 60,  color: '#9945FF' },
    { id: 'PAX2', id2: 'PAX', x: 2, y: 70, h: 100, color: 'var(--brand-blue)' },
    { id: 'LP',   x: 2, y: 180, h: 40, color: 'var(--brand-green)' },
  ];
  const flows = [
    { from: 0, to: 1, size: 100, label: '$3,200' },
    { from: 0, to: 2, size: 60,  label: '$1,900' },
    { from: 1, to: 3, size: 60,  label: '$1,920' },
    { from: 1, to: 4, size: 40,  label: '$1,280' },
    { from: 2, to: 4, size: 60,  label: '$1,900' },
    { from: 2, to: 5, size: 20,  label: '$640' },
  ];
  const W = 560, H = 260, pad = 8, colW = W / 3;
  const nodeW = 14;
  const [hover, setHover] = useState(null);

  const getNode = (i) => nodes[i];
  const nodeX = n => n.x * colW + (n.x === 0 ? 0 : n.x === 2 ? colW - nodeW : colW/2 - nodeW/2);
  const nodeY = n => pad + n.y;

  const flowPath = (f) => {
    const a = getNode(f.from), b = getNode(f.to);
    const ax = nodeX(a) + nodeW, ay = nodeY(a) + a.h / 2;
    const bx = nodeX(b), by = nodeY(b) + b.h / 2;
    const mx = (ax + bx) / 2;
    return `M ${ax} ${ay - f.size/2} C ${mx} ${ay - f.size/2} ${mx} ${by - f.size/2} ${bx} ${by - f.size/2} L ${bx} ${by + f.size/2} C ${mx} ${by + f.size/2} ${mx} ${ay + f.size/2} ${ax} ${ay + f.size/2} Z`;
  };

  return (
    <div style={{ width: '100%', maxWidth: 600, padding: '16px 18px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 10 }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Value flow · last 7 trades</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>total routed · $12,480</div>
      </div>
      <svg width="100%" viewBox={`0 0 ${W} ${H}`}>
        {flows.map((f, i) => (
          <path key={i} d={flowPath(f)}
            fill={getNode(f.from).color}
            opacity={hover === null ? 0.35 : hover === i ? 0.85 : 0.12}
            style={{ transition: 'opacity 180ms', cursor: 'pointer' }}
            onMouseEnter={() => setHover(i)} onMouseLeave={() => setHover(null)} />
        ))}
        {nodes.map((n, i) => (
          <g key={i}>
            <rect x={nodeX(n)} y={nodeY(n)} width={nodeW} height={n.h} fill={n.color} rx="2" />
            <text x={n.x === 0 ? nodeX(n) + nodeW + 8 : n.x === 2 ? nodeX(n) - 8 : nodeX(n) + nodeW + 8}
              y={nodeY(n) + n.h / 2 + 4}
              fill="var(--fg)" fontFamily="JetBrains Mono" fontSize="10" letterSpacing="0.04em"
              textAnchor={n.x === 2 ? 'end' : 'start'}>{n.id2 || n.id}</text>
          </g>
        ))}
        {hover !== null && (
          <g>
            {(() => {
              const f = flows[hover];
              const a = getNode(f.from), b = getNode(f.to);
              const mx = (nodeX(a) + nodeW + nodeX(b)) / 2;
              const my = (nodeY(a) + a.h/2 + nodeY(b) + b.h/2) / 2;
              return (
                <g transform={`translate(${mx},${my})`}>
                  <rect x="-40" y="-10" width="80" height="20" rx="4" fill="var(--bg)" stroke="rgba(255,255,255,0.15)" />
                  <text x="0" y="4" textAnchor="middle" fontFamily="JetBrains Mono" fontSize="10" fill="var(--fg)">{f.label}</text>
                </g>
              );
            })()}
          </g>
        )}
      </svg>
      <div style={{ display: 'flex', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--fg-subtle)', letterSpacing: '0.12em', marginTop: 6, padding: '0 4px' }}>
        <span>SOURCE</span><span>INTERMEDIATE</span><span>DESTINATION</span>
      </div>
    </div>
  );
}

export default TradePathSankey;
