"use client";
import { useEffect, useState } from "react";

export function ReactionRail() {
  const emojis = [
    { k: 'bull',   s: '🐂', c: 'var(--brand-green)' },
    { k: 'bear',   s: '🐻', c: 'var(--brand-red)' },
    { k: 'rocket', s: '🚀', c: 'var(--brand-orange)' },
    { k: 'skull',  s: '💀', c: '#828282' },
    { k: 'fire',   s: '🔥', c: '#FF6B35' },
  ];
  const [counts, setCounts] = useState({ bull: 142, bear: 38, rocket: 92, skull: 12, fire: 56 });
  const [bursts, setBursts] = useState([]); // {k, id, x}

  useEffect(() => {
    const iv = setInterval(() => {
      const pick = emojis[Math.floor(Math.random() * emojis.length)];
      const id = Math.random();
      setCounts(c => ({ ...c, [pick.k]: c[pick.k] + 1 }));
      setBursts(b => [...b, { k: pick.k, id, x: Math.random() * 100 }]);
      setTimeout(() => setBursts(b => b.filter(x => x.id !== id)), 1600);
    }, 900);
    return () => clearInterval(iv);
  }, []);

  const total = Object.values(counts).reduce((a, b) => a + b, 0);
  const [mine, setMine] = useState(null);

  return (
    <div style={{ width: '100%', maxWidth: 480, padding: '16px 18px', background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14, position: 'relative' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: 14 }}>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Market reactions · BTC breaks $68K</div>
        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>{total} live</div>
      </div>
      <div style={{ position: 'relative', height: 60, overflow: 'hidden', marginBottom: 12 }}>
        {bursts.map(b => {
          const em = emojis.find(e => e.k === b.k);
          return (
            <div key={b.id} style={{
              position: 'absolute', left: `${b.x}%`, bottom: 0,
              fontSize: 22, animation: 'paxBurst 1.6s ease-out forwards',
              pointerEvents: 'none',
            }}>{em.s}</div>
          );
        })}
      </div>
      <style>{`@keyframes paxBurst { 0% { transform: translateY(0) scale(0.4); opacity: 0; } 20% { transform: translateY(-10px) scale(1.2); opacity: 1; } 100% { transform: translateY(-50px) scale(0.8); opacity: 0; } }`}</style>
      <div style={{ display: 'flex', gap: 6 }}>
        {emojis.map(e => {
          const n = counts[e.k];
          const pct = (n / total) * 100;
          const active = mine === e.k;
          return (
            <button key={e.k} onClick={() => setMine(e.k)} style={{
              flex: 1, padding: '8px 6px', borderRadius: 10,
              background: active ? 'var(--bg)' : 'transparent',
              border: `1px solid ${active ? e.c : 'var(--border-soft)'}`,
              cursor: 'pointer', display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 3,
              transition: 'all 180ms',
            }}>
              <div style={{ fontSize: 18 }}>{e.s}</div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg)', fontWeight: 500 }}>{n.toLocaleString()}</div>
              <div style={{ width: '100%', height: 2, background: 'rgba(255,255,255,0.05)', borderRadius: 1, overflow: 'hidden' }}>
                <div style={{ width: `${pct}%`, height: '100%', background: e.c, transition: 'width 400ms' }} />
              </div>
            </button>
          );
        })}
      </div>
    </div>
  );
}

export default ReactionRail;
