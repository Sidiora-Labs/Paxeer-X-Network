"use client";
import { useRef, useState } from "react";

// Push the puck all the way to confirm; bounces back if released early.

export function ConfirmSlider({ label = "Slide to confirm swap", confirmedLabel = "Confirmed", accent = "var(--accent-solid)" }) {
  const [pct, setPct] = useState(0);
  const [done, setDone] = useState(false);
  const [dragging, setDragging] = useState(false);
  const trackRef = useRef(null);

  const WIDTH = 340;
  const HANDLE = 48;
  const max = WIDTH - HANDLE - 8;

  const onDown = (e) => {
    if (done) return;
    setDragging(true);
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onMove = (e) => {
    if (!dragging || done) return;
    const rect = trackRef.current.getBoundingClientRect();
    const x = Math.max(0, Math.min(max, e.clientX - rect.left - HANDLE / 2));
    setPct(x / max);
  };
  const onUp = () => {
    if (!dragging) return;
    setDragging(false);
    if (pct > 0.92) {
      setPct(1); setDone(true);
      setTimeout(() => { setDone(false); setPct(0); }, 2200);
    } else {
      setPct(0);
    }
  };

  const handleX = pct * max;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 24, alignItems: 'center' }}>
      {/* summary card */}
      <div style={{
        width: 340, padding: '14px 16px',
        background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
        borderRadius: 12, display: 'flex', flexDirection: 'column', gap: 8,
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>You swap</span>
          <span style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 18 }}>500 USDC</span>
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>You receive</span>
          <span style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 18, color: 'var(--color-blue-200)' }}>1,036.92 PAX</span>
        </div>
      </div>

      {/* slider track */}
      <div
        ref={trackRef}
        style={{
          position: 'relative', width: WIDTH, height: HANDLE + 8,
          background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
          borderRadius: 999, overflow: 'hidden',
          userSelect: 'none', touchAction: 'none',
        }}
        onPointerMove={onMove}
        onPointerUp={onUp}
      >
        {/* fill */}
        <div style={{
          position: 'absolute', inset: 4,
          width: `calc(${pct * 100}% - ${pct * 8}px)`,
          background: `linear-gradient(90deg, ${accent}, #8FA8FF)`,
          borderRadius: 999,
          transition: dragging ? 'none' : 'width 300ms cubic-bezier(0.4,0,0.2,1)',
          opacity: 0.9,
        }} />
        {/* label */}
        <div style={{
          position: 'absolute', inset: 0, display: 'grid', placeItems: 'center',
          fontFamily: 'var(--font-mono)', fontSize: 11, letterSpacing: '0.14em', textTransform: 'uppercase',
          color: pct > 0.5 ? '#fff' : 'var(--fg-muted)',
          pointerEvents: 'none',
        }}>
          {done ? confirmedLabel : label}
        </div>
        {/* handle */}
        <div
          onPointerDown={onDown}
          style={{
            position: 'absolute', top: 4, left: 4 + handleX,
            width: HANDLE, height: HANDLE, borderRadius: 999,
            background: done ? 'var(--success)' : '#fff',
            display: 'grid', placeItems: 'center',
            cursor: done ? 'default' : (dragging ? 'grabbing' : 'grab'),
            boxShadow: '0 4px 12px rgba(0,0,0,0.4)',
            transition: dragging ? 'none' : 'left 300ms cubic-bezier(0.4,0,0.2,1), background 200ms',
            touchAction: 'none',
          }}
        >
          {done ? (
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="#fff" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round"><path d="M20 6L9 17l-5-5"/></svg>
          ) : (
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="var(--brand-blue)" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round"><path d="M5 12h14M12 5l7 7-7 7"/></svg>
          )}
        </div>
      </div>
    </div>
  );
}

export default ConfirmSlider;
