"use client";
import * as React from "react";


export function DepthChart() {
  const MID = 0.4821;
  // build bids (descending price) and asks (ascending), cumulative size
  const N = 20;
  let cumB = 0, cumA = 0;
  const bids = Array.from({ length: N }).map((_, i) => {
    const p = MID - (i + 1) * 0.0005;
    const s = 200 + (Math.sin(i * 1.3) + 1) * 800 + Math.random() * 200;
    cumB += s;
    return { p, s: cumB };
  });
  const asks = Array.from({ length: N }).map((_, i) => {
    const p = MID + (i + 1) * 0.0005;
    const s = 200 + (Math.cos(i * 1.1) + 1) * 800 + Math.random() * 200;
    cumA += s;
    return { p, s: cumA };
  });
  const maxS = Math.max(cumB, cumA);
  const W = 360, H = 160;

  const toXBid = i => (1 - i / (N - 1)) * (W / 2);
  const toXAsk = i => W / 2 + (i / (N - 1)) * (W / 2);
  const toY = s => H - (s / maxS) * (H - 10);

  const bidPts = bids.map((b, i) => `${toXBid(i)},${toY(b.s)}`).join(' ');
  const askPts = asks.map((a, i) => `${toXAsk(i)},${toY(a.s)}`).join(' ');

  return (
    <div style={{ width: 380, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 12, overflow: 'hidden' }}>
      <div style={{ padding: '10px 14px', borderBottom: '1px solid var(--border-soft)', display: 'flex', justifyContent: 'space-between' }}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>PAX/USDC · depth</span>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--color-blue-200)' }}>${MID.toFixed(4)}</span>
      </div>
      <svg width={W} height={H + 30} style={{ display: 'block', padding: '10px 10px 0' }}>
        <defs>
          <linearGradient id="depthBid" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--brand-green)" stopOpacity="0.45" />
            <stop offset="100%" stopColor="var(--brand-green)" stopOpacity="0" />
          </linearGradient>
          <linearGradient id="depthAsk" x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor="var(--brand-red)" stopOpacity="0.45" />
            <stop offset="100%" stopColor="var(--brand-red)" stopOpacity="0" />
          </linearGradient>
        </defs>
        {/* bids */}
        <polyline points={`0,${H} ${bidPts} ${W/2},${H}`} fill="url(#depthBid)" />
        <polyline points={bidPts} fill="none" stroke="var(--brand-green)" strokeWidth="1.5" />
        {/* asks */}
        <polyline points={`${W/2},${H} ${askPts} ${W},${H}`} fill="url(#depthAsk)" />
        <polyline points={askPts} fill="none" stroke="var(--brand-red)" strokeWidth="1.5" />
        {/* mid line */}
        <line x1={W/2} y1="0" x2={W/2} y2={H} stroke="rgba(255,255,255,0.2)" strokeDasharray="2 3" />
        {/* x labels */}
        <text x="4" y={H + 18} fill="#828282" fontFamily="JetBrains Mono" fontSize="9">{bids[N-1].p.toFixed(4)}</text>
        <text x={W/2} y={H + 18} fill="#8FA8FF" fontFamily="JetBrains Mono" fontSize="9" textAnchor="middle">{MID.toFixed(4)}</text>
        <text x={W - 4} y={H + 18} fill="#828282" fontFamily="JetBrains Mono" fontSize="9" textAnchor="end">{asks[N-1].p.toFixed(4)}</text>
      </svg>
    </div>
  );
}

export default DepthChart;
