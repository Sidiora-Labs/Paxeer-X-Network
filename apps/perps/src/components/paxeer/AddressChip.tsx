"use client";
import { useState } from "react";
import { Icon, shortAddr } from "./_shared";

// QR, ENS name, balance, and quick actions.

export function AddressChip() {
  const [open, setOpen] = useState(false);
  const addr = '0x7a3f2E4cD8B1a9F02E7c4AaB3e09a82B9b2c';
  const ens = 'vitalik.paxeer';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 20, alignItems: 'center' }}>
      <div
        onMouseEnter={() => setOpen(true)}
        onMouseLeave={() => setOpen(false)}
        style={{
          width: open ? 340 : 240,
          background: 'var(--bg-subtle)',
          border: '1px solid var(--border-strong)',
          borderRadius: 16,
          overflow: 'hidden',
          transition: 'width 400ms cubic-bezier(0.4,0,0.2,1), height 400ms cubic-bezier(0.4,0,0.2,1)',
          height: open ? 220 : 52,
        }}
      >
        {/* header row */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '10px 12px', height: 52, boxSizing: 'border-box' }}>
          <div style={{
            width: 28, height: 28, borderRadius: 999,
            background: 'conic-gradient(from 45deg, var(--brand-blue), #8FA8FF, var(--brand-blue))',
            flexShrink: 0,
          }} />
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontSize: 13, fontWeight: 500, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{ens}</div>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', marginTop: 1 }}>{shortAddr(addr)}</div>
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.12em', color: 'var(--fg-subtle)', textTransform: 'uppercase', opacity: open ? 0 : 1, transition: 'opacity 200ms' }}>
            hover ↓
          </div>
        </div>

        {open && (
          <div className="gx-fade-in" style={{ padding: '6px 14px 14px', display: 'grid', gridTemplateColumns: '88px 1fr', gap: 14 }}>
            {/* QR placeholder */}
            <div style={{
              width: 88, height: 88, background: '#fff',
              borderRadius: 8, padding: 6, boxSizing: 'border-box',
              display: 'grid', gridTemplateColumns: 'repeat(8, 1fr)', gap: 1,
            }}>
              {Array.from({ length: 64 }).map((_, i) => (
                <div key={i} style={{
                  background: (i * 31 + (i >> 3) * 7) % 3 === 0 ? 'var(--brand-black)' : '#fff',
                  borderRadius: 1,
                }} />
              ))}
            </div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <div>
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Balance</div>
                <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 20, marginTop: 2 }}>$12,487.42</div>
              </div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)', wordBreak: 'break-all', lineHeight: 1.5 }}>
                {addr}
              </div>
              <div style={{ display: 'flex', gap: 6, marginTop: 2 }}>
                <button style={btnMini}>
                  <Icon.copy /> Copy
                </button>
                <button style={btnMini}>
                  <Icon.send /> Send
                </button>
                <button style={btnMini}>
                  <Icon.qr /> QR
                </button>
              </div>
            </div>
          </div>
        )}
      </div>

      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', letterSpacing: '0.14em', textTransform: 'uppercase' }}>
        hover or tap to expand
      </div>
    </div>
  );
}

const btnMini = {
  display: 'inline-flex', alignItems: 'center', gap: 4,
  padding: '4px 8px', fontSize: 11,
  background: 'var(--bg)', color: 'var(--fg-muted)',
  border: '1px solid var(--border-soft)', borderRadius: 6,
  fontFamily: 'var(--font-body)', cursor: 'pointer',
};

export default AddressChip;
