"use client";
import { useRef, useState } from "react";
import { TOKENS, fmt } from "./_shared";

// Two token "pucks" sit in a field; drag one onto the other to initiate a swap.
// The closer they get, the stronger the visual attraction (line thickens, colors bloom).

export function MagneticSwap() {
  const tokens = TOKENS;
  const [from, setFrom] = useState(tokens[1]); // USDC
  const [to, setTo] = useState(tokens[0]);     // PAX
  const [amount, setAmount] = useState(500);
  const [dragPos, setDragPos] = useState(null); // {x, y} while dragging
  const [swapping, setSwapping] = useState(false);
  const containerRef = useRef(null);

  const OUT = ((amount * from.price) / to.price);
  const fromPos = { x: 80, y: 120 };
  const toPos = { x: 280, y: 120 };

  // distance between current drag pos and target
  const dist = dragPos ? Math.hypot(dragPos.x - toPos.x, dragPos.y - toPos.y) : 9999;
  const attraction = Math.max(0, Math.min(1, 1 - dist / 140));

  const onPointerDown = (e) => {
    const rect = containerRef.current.getBoundingClientRect();
    setDragPos({ x: e.clientX - rect.left, y: e.clientY - rect.top });
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e) => {
    if (!dragPos) return;
    const rect = containerRef.current.getBoundingClientRect();
    setDragPos({ x: e.clientX - rect.left, y: e.clientY - rect.top });
  };
  const onPointerUp = () => {
    if (dragPos && dist < 60) {
      // trigger swap
      setSwapping(true);
      setTimeout(() => {
        setFrom(to); setTo(from);
        setSwapping(false);
      }, 400);
    }
    setDragPos(null);
  };

  const activePos = dragPos || fromPos;

  return (
    <div style={{ width: 380, display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div
        ref={containerRef}
        style={{
          position: 'relative', width: 380, height: 240,
          background: 'radial-gradient(circle at 50% 50%, rgba(0,76,237,0.08), transparent 70%)',
          borderRadius: 16,
          overflow: 'hidden',
          touchAction: 'none',
          userSelect: 'none',
        }}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerUp}
        onPointerLeave={onPointerUp}
      >
        {/* attraction line */}
        <svg width="380" height="240" style={{ position: 'absolute', inset: 0, pointerEvents: 'none' }}>
          <defs>
            <linearGradient id="magLine" x1="0" y1="0" x2="1" y2="0">
              <stop offset="0%" stopColor={from.color} />
              <stop offset="100%" stopColor={to.color} />
            </linearGradient>
          </defs>
          {dragPos && (
            <line
              x1={activePos.x} y1={activePos.y} x2={toPos.x} y2={toPos.y}
              stroke="url(#magLine)"
              strokeWidth={1 + attraction * 5}
              strokeDasharray={attraction > 0.5 ? "0" : "4 4"}
              opacity={0.3 + attraction * 0.7}
            />
          )}
          {/* target ring */}
          <circle cx={toPos.x} cy={toPos.y} r={32 + attraction * 16} fill="none" stroke={to.color} strokeWidth="1.5" opacity={0.3 + attraction * 0.7} />
        </svg>

        {/* From puck (draggable) */}
        <div
          onPointerDown={onPointerDown}
          style={{
            position: 'absolute',
            left: activePos.x - 36, top: activePos.y - 36,
            width: 72, height: 72, borderRadius: 999,
            background: `radial-gradient(circle at 30% 25%, ${from.color}, ${from.color}dd)`,
            border: '3px solid #fff',
            boxShadow: dragPos ? `0 0 0 ${4 + attraction * 8}px ${from.color}30, 0 12px 32px rgba(0,0,0,0.5)` : '0 8px 20px rgba(0,0,0,0.4)',
            cursor: dragPos ? 'grabbing' : 'grab',
            display: 'grid', placeItems: 'center',
            color: '#fff', fontWeight: 700,
            fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
            fontSize: 22,
            transition: dragPos ? 'box-shadow 150ms' : 'all 400ms cubic-bezier(0.4,0,0.2,1)',
            zIndex: 5,
            touchAction: 'none',
          }}
        >
          {from.sym[0]}
        </div>

        {/* To puck (target) */}
        <div style={{
          position: 'absolute', left: toPos.x - 32, top: toPos.y - 32,
          width: 64, height: 64, borderRadius: 999,
          background: to.color,
          border: '2px dashed rgba(255,255,255,0.4)',
          display: 'grid', placeItems: 'center',
          color: '#fff', fontWeight: 600,
          fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
          fontSize: 20,
          opacity: 0.85,
          transform: `scale(${1 + attraction * 0.2})`,
          transition: 'transform 200ms',
        }}>
          {to.sym[0]}
        </div>

        {/* hint text */}
        <div style={{
          position: 'absolute', left: 0, right: 0, bottom: 14,
          textAlign: 'center',
          fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em',
          color: 'var(--fg-subtle)', textTransform: 'uppercase',
          opacity: dragPos ? 0 : 1, transition: 'opacity 200ms',
        }}>
          drag {from.sym} → {to.sym}
        </div>

        {/* amount overlay */}
        <div style={{
          position: 'absolute', top: 14, left: 14,
          padding: '6px 10px', background: 'rgba(11,11,11,0.7)',
          border: '1px solid var(--border-soft)', borderRadius: 8,
          fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)',
        }}>
          {fmt(amount, 0)} {from.sym}
        </div>
        <div style={{
          position: 'absolute', top: 14, right: 14,
          padding: '6px 10px', background: 'rgba(11,11,11,0.7)',
          border: '1px solid var(--border-soft)', borderRadius: 8,
          fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)',
        }}>
          ≈ {fmt(OUT, 2)} {to.sym}
        </div>
      </div>

      {/* amount slider */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '0 4px' }}>
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', letterSpacing: '0.14em', textTransform: 'uppercase' }}>Amount</span>
        <input type="range" min="10" max="1000" step="10" value={amount}
          onChange={e => setAmount(+e.target.value)}
          style={{ flex: 1, accentColor: 'var(--accent-solid)' }} />
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: 'var(--fg)' }}>{amount}</span>
      </div>
    </div>
  );
}

export default MagneticSwap;
