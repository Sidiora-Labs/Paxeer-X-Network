"use client";
import { useRef, useState } from "react";

export function LeverageDial() {
  const [lev, setLev] = useState(10);
  const MIN = 1, MAX = 100;
  const dragRef = useRef({ active: false, lastAngle: 0 });
  const R = 88;

  const angleFromLev = l => -Math.PI * 0.75 + ((l - MIN) / (MAX - MIN)) * Math.PI * 1.5;
  const currentAngle = angleFromLev(lev);

  const onPointerDown = (e) => {
    dragRef.current.active = true;
    dragRef.current.lastAngle = null;
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e) => {
    if (!dragRef.current.active) return;
    const rect = e.currentTarget.getBoundingClientRect();
    const cx = rect.left + rect.width / 2;
    const cy = rect.top + rect.height / 2;
    const ang = Math.atan2(e.clientY - cy, e.clientX - cx);
    if (dragRef.current.lastAngle !== null) {
      let delta = ang - dragRef.current.lastAngle;
      if (delta > Math.PI) delta -= Math.PI * 2;
      if (delta < -Math.PI) delta += Math.PI * 2;
      const d = (delta / (Math.PI * 1.5)) * (MAX - MIN);
      setLev(l => Math.max(MIN, Math.min(MAX, l + d)));
    }
    dragRef.current.lastAngle = ang;
  };
  const onPointerUp = (e) => {
    dragRef.current.active = false;
    e.currentTarget.releasePointerCapture(e.pointerId);
  };

  // ticks around arc
  const ticks = Array.from({ length: 21 }).map((_, i) => {
    const t = i / 20;
    const a = -Math.PI * 0.75 + t * Math.PI * 1.5;
    const big = i % 5 === 0;
    const r1 = R - (big ? 14 : 8);
    const r2 = R - 2;
    return { x1: Math.cos(a)*r1, y1: Math.sin(a)*r1, x2: Math.cos(a)*r2, y2: Math.sin(a)*r2, big };
  });

  const liqRisk = Math.min(1, (lev - 1) / 40); // red zone starts mattering above 20×
  const risk = lev >= 25 ? 'high' : lev >= 10 ? 'med' : 'low';
  const riskColor = risk === 'high' ? 'var(--danger)' : risk === 'med' ? 'var(--warning)' : 'var(--success)';

  // arc path from start up to currentAngle
  const arcPath = () => {
    const large = (currentAngle - (-Math.PI*0.75)) > Math.PI ? 1 : 0;
    const x1 = Math.cos(-Math.PI*0.75) * (R - 6);
    const y1 = Math.sin(-Math.PI*0.75) * (R - 6);
    const x2 = Math.cos(currentAngle) * (R - 6);
    const y2 = Math.sin(currentAngle) * (R - 6);
    return `M ${x1} ${y1} A ${R-6} ${R-6} 0 ${large} 1 ${x2} ${y2}`;
  };

  return (
    <div style={{ width: 300, padding: 20, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 16, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14 }}>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Leverage</div>
      <div
        onPointerDown={onPointerDown} onPointerMove={onPointerMove} onPointerUp={onPointerUp}
        style={{ width: 220, height: 220, position: 'relative', touchAction: 'none', cursor: 'grab' }}>
        <svg width="220" height="220" viewBox="-110 -110 220 220">
          {/* outer ring */}
          <circle cx="0" cy="0" r={R} fill="none" stroke="rgba(255,255,255,0.04)" strokeWidth="1" />
          {/* ticks */}
          {ticks.map((t, i) => (
            <line key={i} x1={t.x1} y1={t.y1} x2={t.x2} y2={t.y2} stroke={t.big ? 'rgba(255,255,255,0.5)' : 'rgba(255,255,255,0.15)'} strokeWidth="1" strokeLinecap="round" />
          ))}
          {/* arc fill */}
          <path d={arcPath()} fill="none" stroke={riskColor} strokeWidth="3" strokeLinecap="round" />
          {/* handle */}
          <circle cx={Math.cos(currentAngle) * (R - 6)} cy={Math.sin(currentAngle) * (R - 6)} r="7" fill={riskColor} stroke="var(--bg-subtle)" strokeWidth="3" />
          {/* inner disc */}
          <circle cx="0" cy="0" r={R - 26} fill="var(--bg)" stroke="rgba(255,255,255,0.06)" strokeWidth="1" />
          {/* label */}
          <text x="0" y="6" textAnchor="middle" fill="var(--fg)" fontFamily="'Paxeer Grand Sans', Inter" fontSize="34" fontWeight="300" letterSpacing="-1">{Math.round(lev)}×</text>
          <text x="0" y="28" textAnchor="middle" fill={riskColor} fontFamily="JetBrains Mono" fontSize="9" letterSpacing="2" textTransform="uppercase">{risk.toUpperCase()} RISK</text>
        </svg>
      </div>
      <div style={{ width: '100%', display: 'flex', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>
        <span>liq. distance · <span style={{ color: 'var(--fg)' }}>{(100 / lev).toFixed(1)}%</span></span>
        <span>margin · <span style={{ color: 'var(--fg)' }}>${(1000 / lev).toFixed(0)}</span></span>
      </div>
      <div style={{ width: '100%', display: 'flex', gap: 6 }}>
        {[1, 5, 10, 25, 50].map(v => (
          <button key={v} onClick={() => setLev(v)} style={{
            flex: 1, padding: '5px 0', background: lev === v ? 'var(--bg)' : 'transparent',
            border: '1px solid var(--border-soft)', borderRadius: 6,
            color: lev === v ? 'var(--fg)' : 'var(--fg-muted)',
            fontFamily: 'var(--font-mono)', fontSize: 10, cursor: 'pointer',
          }}>{v}×</button>
        ))}
      </div>
    </div>
  );
}

export default LeverageDial;
