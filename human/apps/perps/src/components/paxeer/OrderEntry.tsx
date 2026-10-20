"use client";
import { useState } from "react";
import { fmt } from "./_shared";

// leverage slider, and a live P&L preview. Tactile, opinionated.

export function OrderEntry() {
  const [side, setSide] = useState('buy');
  const [type, setType] = useState('limit');
  const [price, setPrice] = useState('0.4821');
  const [size, setSize] = useState('1000');
  const [lev, setLev] = useState(5);

  const notional = (parseFloat(price) || 0) * (parseFloat(size) || 0);
  const margin = notional / lev;
  const liq = side === 'buy'
    ? (parseFloat(price) || 0) * (1 - 0.93 / lev)
    : (parseFloat(price) || 0) * (1 + 0.93 / lev);

  const accent = side === 'buy' ? 'var(--success)' : 'var(--danger)';
  const accentBg = side === 'buy' ? 'rgba(5,193,104,0.14)' : 'rgba(255,90,101,0.14)';

  return (
    <div style={{ width: 340, display: 'flex', flexDirection: 'column', gap: 10 }}>
      {/* buy/sell toggle — fat, tactile */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 4, padding: 4, background: 'var(--bg-subtle)', borderRadius: 12 }}>
        <button onClick={() => setSide('buy')} style={{
          padding: '10px', borderRadius: 8, border: 'none',
          background: side === 'buy' ? 'var(--success)' : 'transparent',
          color: side === 'buy' ? '#fff' : 'var(--fg-muted)',
          fontFamily: 'var(--font-body)', fontSize: 13, fontWeight: 600,
          cursor: 'pointer', transition: 'all 150ms',
          letterSpacing: '0.04em', textTransform: 'uppercase',
        }}>▲ Long</button>
        <button onClick={() => setSide('sell')} style={{
          padding: '10px', borderRadius: 8, border: 'none',
          background: side === 'sell' ? 'var(--danger)' : 'transparent',
          color: side === 'sell' ? '#fff' : 'var(--fg-muted)',
          fontFamily: 'var(--font-body)', fontSize: 13, fontWeight: 600,
          cursor: 'pointer', transition: 'all 150ms',
          letterSpacing: '0.04em', textTransform: 'uppercase',
        }}>▼ Short</button>
      </div>

      {/* type tabs */}
      <div style={{ display: 'flex', gap: 2, padding: 2, background: 'var(--bg-subtle)', borderRadius: 999, width: 'fit-content' }}>
        {['market', 'limit', 'stop'].map(t => (
          <button key={t} onClick={() => setType(t)} style={{
            padding: '5px 12px', borderRadius: 999, border: 'none',
            background: type === t ? 'var(--bg)' : 'transparent',
            color: type === t ? 'var(--fg)' : 'var(--fg-muted)',
            fontFamily: 'var(--font-mono)', fontSize: 10,
            letterSpacing: '0.12em', textTransform: 'uppercase',
            cursor: 'pointer',
          }}>{t}</button>
        ))}
      </div>

      {/* fields */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <div style={fieldBox}>
          <span style={fieldLabel}>Price</span>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
            <input value={price} onChange={e => setPrice(e.target.value)}
              disabled={type === 'market'}
              style={{ ...fieldInput, opacity: type === 'market' ? 0.5 : 1 }} />
            <span style={fieldUnit}>USDC</span>
          </div>
        </div>
        <div style={fieldBox}>
          <span style={fieldLabel}>Size</span>
          <div style={{ display: 'flex', alignItems: 'baseline', gap: 6 }}>
            <input value={size} onChange={e => setSize(e.target.value)} style={fieldInput} />
            <span style={fieldUnit}>PAX</span>
          </div>
        </div>
      </div>

      {/* leverage */}
      <div style={{ ...fieldBox, gap: 8 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between' }}>
          <span style={fieldLabel}>Leverage</span>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 13, fontWeight: 600, color: accent }}>{lev}×</span>
        </div>
        <input type="range" min="1" max="25" step="1" value={lev}
          onChange={e => setLev(+e.target.value)}
          style={{ accentColor: accent, width: '100%' }} />
        <div style={{ display: 'flex', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 9, color: 'var(--fg-subtle)', letterSpacing: '0.08em' }}>
          <span>1×</span><span>5×</span><span>10×</span><span>25×</span>
        </div>
      </div>

      {/* preview */}
      <div style={{
        padding: '10px 12px', borderRadius: 10,
        background: accentBg, border: `1px solid ${accent === 'var(--success)' ? 'rgba(5,193,104,0.3)' : 'rgba(255,90,101,0.3)'}`,
        display: 'flex', flexDirection: 'column', gap: 4,
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)' }}>
          <span>Notional</span><span style={{ color: 'var(--fg)' }}>${fmt(notional, 2)}</span>
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)' }}>
          <span>Margin</span><span style={{ color: 'var(--fg)' }}>${fmt(margin, 2)}</span>
        </div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)' }}>
          <span>Liquidation</span><span style={{ color: accent }}>${fmt(liq, 4)}</span>
        </div>
      </div>

      <button style={{
        padding: '14px', borderRadius: 999, border: 'none',
        background: accent, color: '#fff',
        fontFamily: 'var(--font-body)', fontSize: 14, fontWeight: 600,
        cursor: 'pointer', letterSpacing: '0.02em',
        boxShadow: side === 'buy' ? '0 8px 20px -8px rgba(5,193,104,0.5)' : '0 8px 20px -8px rgba(255,90,101,0.5)',
      }}>
        {side === 'buy' ? 'Open long' : 'Open short'} · {lev}× · {size} PAX
      </button>
    </div>
  );
}

const fieldBox = {
  padding: '10px 12px',
  background: 'var(--bg-subtle)',
  border: '1px solid var(--border-soft)',
  borderRadius: 10,
  display: 'flex', flexDirection: 'column', gap: 4,
};
const fieldLabel = {
  fontFamily: 'var(--font-mono)', fontSize: 9,
  letterSpacing: '0.14em', textTransform: 'uppercase',
  color: 'var(--fg-subtle)',
};
const fieldInput = {
  flex: 1, background: 'none', border: 'none', outline: 'none',
  color: 'var(--fg)', fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
  fontSize: 20, fontWeight: 400, letterSpacing: '-0.01em',
  width: '100%', fontVariantNumeric: 'tabular-nums',
};
const fieldUnit = {
  fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)',
  letterSpacing: '0.08em',
};

export default OrderEntry;
