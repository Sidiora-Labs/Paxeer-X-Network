"use client";
import { useRef, useState } from "react";

export function GestureUnlock() {
  const [path, setPath] = useState([]);
  const [drawing, setDrawing] = useState(false);
  const [status, setStatus] = useState('idle'); // idle, ok, err
  const containerRef = useRef(null);
  const target = [0, 4, 8, 5, 2]; // correct pattern

  const nodeAt = (x, y) => {
    const rect = containerRef.current.getBoundingClientRect();
    const lx = x - rect.left, ly = y - rect.top;
    const step = rect.width / 3;
    for (let i = 0; i < 9; i++) {
      const cx = (i % 3) * step + step / 2;
      const cy = Math.floor(i / 3) * step + step / 2;
      if ((lx - cx) ** 2 + (ly - cy) ** 2 < 30 ** 2) return i;
    }
    return -1;
  };

  const start = (e) => { setDrawing(true); setPath([]); setStatus('idle'); };
  const move = (e) => {
    if (!drawing) return;
    const p = e.touches ? e.touches[0] : e;
    const n = nodeAt(p.clientX, p.clientY);
    if (n !== -1 && !path.includes(n)) setPath([...path, n]);
  };
  const end = () => {
    setDrawing(false);
    if (path.length >= 4) {
      const ok = path.length === target.length && path.every((v, i) => v === target[i]);
      setStatus(ok ? 'ok' : 'err');
      if (!ok) setTimeout(() => { setPath([]); setStatus('idle'); }, 900);
    } else if (path.length > 0) {
      setStatus('err');
      setTimeout(() => { setPath([]); setStatus('idle'); }, 600);
    }
  };

  const pts = path.map(i => {
    const x = (i % 3) / 2 * 100;
    const y = Math.floor(i / 3) / 2 * 100;
    return `${x}%,${y}%`;
  });
  const stroke = status === 'ok' ? 'var(--brand-green)' : status === 'err' ? 'var(--brand-red)' : 'var(--accent-solid)';

  return (
    <div style={{ width: 260, padding: 20, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14, display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 14 }}>
      <div style={{ textAlign: 'center' }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Sign transaction</div>
        <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 14, color: stroke, marginTop: 4 }}>
          {status === 'ok' ? 'Authenticated' : status === 'err' ? 'Pattern mismatch' : 'Draw your pattern'}
        </div>
      </div>
      <div ref={containerRef}
        onPointerDown={start} onPointerMove={move} onPointerUp={end} onPointerLeave={end}
        style={{
          width: 200, height: 200, position: 'relative',
          touchAction: 'none', userSelect: 'none',
        }}>
        <svg style={{ position: 'absolute', inset: 0, width: '100%', height: '100%', pointerEvents: 'none' }}>
          <polyline points={pts.join(' ')} fill="none" stroke={stroke} strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
        </svg>
        {Array.from({ length: 9 }).map((_, i) => {
          const on = path.includes(i);
          return (
            <div key={i} style={{
              position: 'absolute',
              left: `${(i % 3) / 2 * 100}%`, top: `${Math.floor(i / 3) / 2 * 100}%`,
              transform: 'translate(-50%,-50%)',
              width: 42, height: 42, borderRadius: 999,
              background: on ? stroke : 'transparent',
              border: `1.5px solid ${on ? stroke : 'rgba(255,255,255,0.18)'}`,
              display: 'grid', placeItems: 'center',
              transition: 'all 140ms',
            }}>
              {on && <div style={{ width: 10, height: 10, borderRadius: 999, background: 'var(--bg-subtle)' }} />}
            </div>
          );
        })}
      </div>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', letterSpacing: '0.04em' }}>
        hint · ┍━┑
      </div>
    </div>
  );
}

export default GestureUnlock;
