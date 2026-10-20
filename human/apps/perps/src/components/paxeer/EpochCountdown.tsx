"use client";
import { useEffect, useState } from "react";
import { fmt } from "./_shared";

export function EpochCountdown() {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    const iv = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(iv);
  }, []);

  const timers = [
    { label: 'FUNDING',   total: 8 * 3600,     remain: 3 * 3600 + 42 * 60, color: 'var(--brand-blue)', r: 80 },
    { label: 'REWARDS',   total: 24 * 3600,    remain: 9 * 3600 + 12 * 60, color: '#9945FF', r: 62 },
    { label: 'UNLOCK',    total: 7 * 24 * 3600, remain: 2 * 24 * 3600 + 4 * 3600, color: 'var(--brand-orange)', r: 44 },
  ];

  const fmt = (s) => {
    const d = Math.floor(s / 86400);
    const h = Math.floor((s % 86400) / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = s % 60;
    if (d) return `${d}d ${h}h`;
    if (h) return `${h}h ${m}m`;
    return `${m}m ${sec}s`;
  };

  const arc = (r, pct, color) => {
    const a1 = -Math.PI / 2, a2 = a1 + pct * Math.PI * 2;
    const large = pct > 0.5 ? 1 : 0;
    if (pct >= 0.999) return <circle r={r} fill="none" stroke={color} strokeWidth="4" />;
    return <path d={`M 0 ${-r} A ${r} ${r} 0 ${large} 1 ${Math.cos(a2)*r} ${Math.sin(a2)*r}`} fill="none" stroke={color} strokeWidth="4" strokeLinecap="round" />;
  };

  const soonest = timers[0];

  return (
    <div style={{ width: 340, padding: 18, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14 }}>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Epoch timers</div>
      <div style={{ position: 'relative', width: 200, height: 200 }}>
        <svg width="200" height="200" viewBox="-100 -100 200 200">
          {timers.map(t => (
            <g key={t.label}>
              <circle r={t.r} fill="none" stroke="rgba(255,255,255,0.05)" strokeWidth="4" />
              {arc(t.r, t.remain / t.total, t.color)}
            </g>
          ))}
          <text x="0" y="-2" textAnchor="middle" fill="var(--fg)" fontFamily="'Paxeer Grand Sans', Inter" fontSize="22" fontWeight="300" letterSpacing="-1">{fmt(soonest.remain)}</text>
          <text x="0" y="14" textAnchor="middle" fill={soonest.color} fontFamily="JetBrains Mono" fontSize="8" letterSpacing="2">TO {soonest.label}</text>
        </svg>
      </div>
      <div style={{ width: '100%', display: 'flex', flexDirection: 'column', gap: 6 }}>
        {timers.map(t => (
          <div key={t.label} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
            <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <span style={{ width: 8, height: 8, borderRadius: 2, background: t.color }} />
              <span style={{ color: 'var(--fg)' }}>{t.label}</span>
            </span>
            <span style={{ color: 'var(--fg-muted)' }}>{fmt(t.remain)}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

export default EpochCountdown;
