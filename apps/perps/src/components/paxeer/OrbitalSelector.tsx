"use client";
import { useEffect, useState } from "react";
import { TOKENS, fmt } from "./_shared";

// click/hover to select. The selected token snaps to the top anchor.

export function OrbitalSelector() {
  const tokens = TOKENS;
  const [selected, setSelected] = useState(0);
  const [hovered, setHovered] = useState(null);
  const [rotation, setRotation] = useState(0);

  // slow idle rotation
  useEffect(() => {
    let raf;
    let last = performance.now();
    const tick = (t) => {
      const dt = t - last; last = t;
      setRotation(r => r + dt * 0.008);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, []);

  const R = 112; // orbit radius
  const N = tokens.length;
  const current = tokens[selected];

  return (
    <div style={{ position: 'relative', width: 320, height: 320, display: 'grid', placeItems: 'center' }}>
      {/* orbit rings */}
      <svg width="320" height="320" style={{ position: 'absolute', inset: 0 }}>
        <circle cx="160" cy="160" r={R} fill="none" stroke="rgba(255,255,255,0.06)" strokeDasharray="2 4" />
        <circle cx="160" cy="160" r={R - 28} fill="none" stroke="rgba(0,76,237,0.12)" />
        <circle cx="160" cy="160" r={R + 28} fill="none" stroke="rgba(255,255,255,0.03)" />
      </svg>

      {/* top anchor indicator */}
      <div style={{
        position: 'absolute', top: 30, left: '50%', transform: 'translateX(-50%)',
        fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.2em',
        color: 'var(--fg-subtle)', textTransform: 'uppercase',
      }}>selected ▾</div>

      {/* orbiting tokens */}
      {tokens.map((t, i) => {
        // rotate so selected ends up at top (-90deg)
        const baseAngle = (i / N) * 360;
        const offset = -90 - (selected / N) * 360;
        const angle = baseAngle + offset + rotation * 0.06;
        const rad = (angle * Math.PI) / 180;
        const x = Math.cos(rad) * R;
        const y = Math.sin(rad) * R;
        const isSelected = i === selected;
        const isHovered = i === hovered;
        const size = isSelected ? 52 : isHovered ? 44 : 36;
        return (
          <button
            key={t.sym}
            onClick={() => setSelected(i)}
            onMouseEnter={() => setHovered(i)}
            onMouseLeave={() => setHovered(null)}
            style={{
              position: 'absolute', left: '50%', top: '50%',
              transform: `translate(calc(-50% + ${x}px), calc(-50% + ${y}px))`,
              width: size, height: size, borderRadius: 999,
              background: t.color,
              color: '#fff', fontWeight: 600,
              fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
              fontSize: size * 0.42,
              border: isSelected ? '2px solid #fff' : '2px solid rgba(255,255,255,0.1)',
              cursor: 'pointer',
              display: 'grid', placeItems: 'center',
              boxShadow: isSelected ? '0 0 0 6px rgba(0,76,237,0.2), 0 8px 24px rgba(0,76,237,0.4)' : '0 4px 12px rgba(0,0,0,0.3)',
              transition: 'width 300ms cubic-bezier(0.4,0,0.2,1), height 300ms cubic-bezier(0.4,0,0.2,1), box-shadow 300ms, transform 500ms cubic-bezier(0.4,0,0.2,1)',
              zIndex: isSelected ? 10 : 1,
            }}
          >
            {t.sym[0]}
          </button>
        );
      })}

      {/* center readout */}
      <div style={{
        position: 'relative', zIndex: 2,
        width: 140, height: 140, borderRadius: 999,
        background: 'radial-gradient(circle at 50% 0%, rgba(0,76,237,0.16), rgba(11,11,11,0.9) 60%)',
        border: '1px solid var(--border-strong)',
        display: 'flex', flexDirection: 'column',
        alignItems: 'center', justifyContent: 'center',
        backdropFilter: 'blur(6px)',
      }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em', color: 'var(--fg-subtle)', textTransform: 'uppercase' }}>
          {current.name}
        </div>
        <div style={{ fontFamily: 'var(--font-display)', fontSize: 32, fontWeight: 500, letterSpacing: '-0.02em', marginTop: 2 }}>
          {current.sym}
        </div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: current.change >= 0 ? 'var(--success)' : 'var(--danger)', marginTop: 2 }}>
          ${fmt(current.price, current.price < 10 ? 4 : 2)} <span style={{ color: 'var(--fg-subtle)' }}>·</span> {current.change >= 0 ? '▲' : '▼'} {Math.abs(current.change).toFixed(2)}%
        </div>
      </div>
    </div>
  );
}

export default OrbitalSelector;
