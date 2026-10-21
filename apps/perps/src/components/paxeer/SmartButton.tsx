"use client";
import { useState } from "react";
import { Icon } from "./_shared";

// Click to cycle through the arc: idle → loading → success → idle.

export function SmartButton() {
  const [state, setState] = useState('idle');
  const click = () => {
    if (state !== 'idle') return;
    setState('loading');
    setTimeout(() => setState('success'), 1400);
    setTimeout(() => setState('idle'), 2800);
  };

  const content = {
    idle:    { bg: 'var(--accent-solid)',  fg: '#fff', icon: <Icon.zap />, label: 'Execute swap' },
    loading: { bg: 'var(--accent-solid)',  fg: '#fff', icon: <span className="gx-spin" />, label: 'Submitting…' },
    success: { bg: 'var(--success)', fg: '#fff', icon: <Icon.check />, label: 'Confirmed · block 8,492,104' },
    error:   { bg: 'var(--danger)',  fg: '#fff', icon: <Icon.x />, label: 'Rejected' },
  }[state];

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, alignItems: 'center' }}>
      <style>{`
        @keyframes gxSpin { to { transform: rotate(360deg); } }
        .gx-spin { width: 14px; height: 14px; border: 2px solid rgba(255,255,255,0.3); border-top-color: #fff; border-radius: 999px; animation: gxSpin 700ms linear infinite; display: inline-block; }
      `}</style>
      <button
        onClick={click}
        style={{
          display: 'inline-flex', alignItems: 'center', gap: 10,
          padding: '14px 24px',
          background: content.bg, color: content.fg,
          border: 'none', borderRadius: 999,
          fontFamily: 'var(--font-body)', fontSize: 15, fontWeight: 500,
          cursor: state === 'idle' ? 'pointer' : 'default',
          minWidth: 220, justifyContent: 'center',
          transition: 'background 300ms cubic-bezier(0.4,0,0.2,1), min-width 300ms',
          boxShadow: state === 'idle' ? '0 8px 20px -8px rgba(0,76,237,0.5)' : 'none',
        }}>
        {content.icon}
        <span>{content.label}</span>
      </button>

      {/* state chips */}
      <div style={{ display: 'flex', gap: 8 }}>
        {['idle', 'loading', 'success', 'error'].map(s => (
          <button key={s} onClick={() => setState(s)} style={{
            padding: '5px 10px',
            background: state === s ? 'var(--bg-subtle)' : 'transparent',
            border: '1px solid var(--border-soft)',
            borderRadius: 999,
            fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.1em',
            color: state === s ? 'var(--fg)' : 'var(--fg-muted)',
            textTransform: 'uppercase', cursor: 'pointer',
          }}>{s}</button>
        ))}
      </div>
    </div>
  );
}

export default SmartButton;
