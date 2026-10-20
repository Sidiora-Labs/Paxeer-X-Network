"use client";
import { useState } from "react";

export function TraderCard() {
  const spark = [12,18,14,22,28,24,34,30,42,38,48,56,52,64,72,68,80,76,88];
  const max = Math.max(...spark), min = Math.min(...spark);
  const W = 200, H = 40;
  const pts = spark.map((v, i) => `${(i / (spark.length - 1)) * W},${H - ((v - min) / (max - min)) * H}`).join(' ');

  const [follow, setFollow] = useState(false);

  const Ring = ({ v, label, color }) => {
    const C = 22 * 2 * Math.PI;
    return (
      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2 }}>
        <svg width="52" height="52" viewBox="-26 -26 52 52">
          <circle r="22" fill="none" stroke="rgba(255,255,255,0.06)" strokeWidth="3" />
          <circle r="22" fill="none" stroke={color} strokeWidth="3" strokeLinecap="round"
            strokeDasharray={`${(v/100) * C} ${C}`} transform="rotate(-90)" />
          <text textAnchor="middle" y="4" fontFamily="'Paxeer Grand Sans', Inter" fontSize="13" fill="var(--fg)">{v}</text>
        </svg>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 8, letterSpacing: '0.1em', color: 'var(--fg-subtle)', textTransform: 'uppercase' }}>{label}</div>
      </div>
    );
  };

  return (
    <div style={{ width: 340, padding: 18, background: 'var(--bg-elevated)', border: '1px solid var(--border-strong)', borderRadius: 14, boxShadow: 'var(--shadow-04)' }}>
      <div style={{ display: 'flex', gap: 14, alignItems: 'center' }}>
        <div style={{
          width: 54, height: 54, borderRadius: 12,
          background: 'linear-gradient(135deg, var(--brand-blue), #9945FF)',
          display: 'grid', placeItems: 'center',
          fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
          fontSize: 22, color: '#fff', fontWeight: 400,
        }}>P</div>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            <span style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 17, color: 'var(--fg)' }}>pax.trader.eth</span>
            <svg width="12" height="12" viewBox="0 0 24 24" fill="var(--brand-blue)"><path d="M12 2L15 8L22 9L17 14L18 21L12 18L6 21L7 14L2 9L9 8Z"/></svg>
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>rank #2 · 1,482 followers</div>
        </div>
        <button onClick={() => setFollow(!follow)} style={{
          padding: '6px 14px', borderRadius: 999,
          background: follow ? 'transparent' : 'var(--accent-solid)',
          color: follow ? 'var(--fg)' : '#fff',
          border: follow ? '1px solid var(--border-soft)' : 'none',
          fontFamily: 'var(--font-body)', fontSize: 11, cursor: 'pointer',
        }}>{follow ? 'Following' : 'Copy trade'}</button>
      </div>
      <div style={{ margin: '14px 0 10px' }}>
        <svg width="100%" viewBox={`0 0 ${W} ${H}`}>
          <defs><linearGradient id="traderSpark" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="var(--brand-green)" stopOpacity="0.35" /><stop offset="100%" stopColor="var(--brand-green)" stopOpacity="0" /></linearGradient></defs>
          <polyline points={`0,${H} ${pts} ${W},${H}`} fill="url(#traderSpark)" />
          <polyline points={pts} fill="none" stroke="var(--brand-green)" strokeWidth="1.5" />
        </svg>
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-around', paddingTop: 6, borderTop: '1px solid var(--border-soft)' }}>
        <Ring v={68} label="win rate" color="var(--brand-green)" />
        <Ring v={82} label="sharpe" color="#8FA8FF" />
        <Ring v={45} label="drawdown" color="var(--brand-orange)" />
      </div>
      <div style={{ marginTop: 12, padding: '8px 10px', background: 'var(--bg-subtle)', borderRadius: 8, display: 'flex', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 10 }}>
        <span style={{ color: 'var(--fg-subtle)' }}>7d P&amp;L</span>
        <span style={{ color: 'var(--success)' }}>+$52,482.10</span>
      </div>
    </div>
  );
}

export default TraderCard;
