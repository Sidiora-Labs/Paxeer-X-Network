"use client";
import { useState } from "react";

export function CommandPalette() {
  const items = [
    { g: 'Markets',  label: 'PAX/USDC',    hint: '$0.4821', icon: '◎' },
    { g: 'Markets',  label: 'ETH-PERP',    hint: '$3,214.88 · 10×', icon: '◎' },
    { g: 'Markets',  label: 'SOL/USDC',    hint: '$138.42', icon: '◎' },
    { g: 'Actions',  label: 'Deposit USDC', hint: '↓', icon: '⇣' },
    { g: 'Actions',  label: 'Close all positions', hint: '4 open', icon: '✕' },
    { g: 'Actions',  label: 'Claim rewards', hint: '42.4 PAX', icon: '★' },
    { g: 'Pages',    label: 'Leaderboard', hint: 'rank · 42', icon: '#' },
    { g: 'Pages',    label: 'Portfolio',   hint: '$24.8K', icon: '◫' },
    { g: 'Pages',    label: 'Settings',    hint: 'keyboard shortcuts', icon: '⚙' },
  ];
  const [q, setQ] = useState('');
  const [sel, setSel] = useState(0);
  const filtered = items.filter(i => i.label.toLowerCase().includes(q.toLowerCase()));
  const grouped = filtered.reduce((a, it) => ({ ...a, [it.g]: [...(a[it.g] || []), it] }), {});

  return (
    <div style={{
      width: 480, background: 'var(--bg-elevated)',
      border: '1px solid var(--border-strong)', borderRadius: 14,
      boxShadow: 'var(--shadow-06)', overflow: 'hidden',
    }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '14px 18px', borderBottom: '1px solid var(--border-soft)' }}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ color: 'var(--fg-muted)' }}><circle cx="11" cy="11" r="8"/><path d="M21 21l-4-4"/></svg>
        <input autoFocus value={q} onChange={e => { setQ(e.target.value); setSel(0); }}
          placeholder="Search markets, actions, pages…"
          style={{
            flex: 1, background: 'none', border: 'none', outline: 'none',
            color: 'var(--fg)', fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
            fontSize: 15, letterSpacing: '-0.01em',
          }} />
        <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', padding: '2px 6px', border: '1px solid var(--border-soft)', borderRadius: 4 }}>ESC</span>
      </div>
      <div style={{ maxHeight: 340, overflow: 'auto', padding: 6 }}>
        {Object.entries(grouped).map(([g, list]) => (
          <div key={g} style={{ marginBottom: 4 }}>
            <div style={{ padding: '6px 10px 4px', fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>{g}</div>
            {list.map((it, i) => {
              const globalI = filtered.indexOf(it);
              const active = globalI === sel;
              return (
                <div key={it.label}
                  onMouseEnter={() => setSel(globalI)}
                  style={{
                    display: 'flex', alignItems: 'center', gap: 12,
                    padding: '9px 10px', borderRadius: 8,
                    background: active ? 'var(--bg-subtle)' : 'transparent',
                    cursor: 'pointer',
                  }}>
                  <div style={{ width: 22, height: 22, borderRadius: 6, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', display: 'grid', placeItems: 'center', color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)', fontSize: 12 }}>{it.icon}</div>
                  <span style={{ flex: 1, fontFamily: 'var(--font-body)', fontSize: 13, color: 'var(--fg)' }}>{it.label}</span>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>{it.hint}</span>
                  {active && <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" style={{ color: 'var(--color-blue-200)' }}><path d="M5 12h14M12 5l7 7-7 7"/></svg>}
                </div>
              );
            })}
          </div>
        ))}
        {filtered.length === 0 && <div style={{ padding: 30, textAlign: 'center', fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-subtle)' }}>no results</div>}
      </div>
      <div style={{ padding: '8px 14px', borderTop: '1px solid var(--border-soft)', display: 'flex', gap: 16, fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)' }}>
        <span>↑↓ navigate</span>
        <span>↵ select</span>
        <span style={{ marginLeft: 'auto' }}>powered by paxeer</span>
      </div>
    </div>
  );
}

export default CommandPalette;
