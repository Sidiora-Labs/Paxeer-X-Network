"use client";
import * as React from "react";
import { fmt } from "./_shared";


export function PositionCard({ data }) {
  const d = data || {
    sym: 'PAX', side: 'long', size: 5000, entry: 0.4650, mark: 0.4821, lev: 5,
    liq: 0.3720, funding: -0.0042, pnl: 85.50, pnlPct: 3.68,
  };
  const isLong = d.side === 'long';
  const color = d.pnl >= 0 ? 'var(--success)' : 'var(--danger)';

  // liq-distance: how close mark is to liquidation (0=safe, 1=dead)
  const range = Math.abs(d.entry - d.liq);
  const danger = Math.max(0, Math.min(1, (isLong ? (d.mark - d.liq) : (d.liq - d.mark)) / range));
  const safety = 1 - danger; // 0 = liquidating

  return (
    <div style={{
      width: 380, padding: 18,
      background: 'var(--bg-subtle)',
      border: '1px solid var(--border-soft)',
      borderRadius: 16,
      display: 'flex', flexDirection: 'column', gap: 12,
    }}>
      {/* top row */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <div style={{
          width: 36, height: 36, borderRadius: 10,
          background: 'var(--brand-blue)',
          display: 'grid', placeItems: 'center',
          fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
          fontWeight: 600, fontSize: 16, color: '#fff',
        }}>
          {d.sym[0]}
        </div>
        <div style={{ flex: 1 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ fontSize: 15, fontWeight: 500 }}>{d.sym}/USDC</span>
            <span style={{
              padding: '2px 7px', borderRadius: 4,
              background: isLong ? 'rgba(5,193,104,0.16)' : 'rgba(255,90,101,0.16)',
              color: isLong ? 'var(--success)' : 'var(--danger)',
              fontFamily: 'var(--font-mono)', fontSize: 9,
              letterSpacing: '0.1em', textTransform: 'uppercase',
            }}>{d.side} · {d.lev}×</span>
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', marginTop: 2, letterSpacing: '0.04em' }}>
            {fmt(d.size, 0)} PAX · ${fmt(d.size * d.entry, 2)} notional
          </div>
        </div>
        <div style={{ textAlign: 'right' }}>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 22, fontWeight: 500, color, letterSpacing: '-0.01em' }}>
            {d.pnl >= 0 ? '+' : ''}${fmt(d.pnl, 2)}
          </div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color, marginTop: 1 }}>
            {d.pnlPct >= 0 ? '▲' : '▼'} {Math.abs(d.pnlPct).toFixed(2)}%
          </div>
        </div>
      </div>

      {/* liquidation bar */}
      <div>
        <div style={{ display: 'flex', justifyContent: 'space-between', fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.12em', textTransform: 'uppercase', color: 'var(--fg-subtle)', marginBottom: 6 }}>
          <span>Liq ${d.liq.toFixed(4)}</span>
          <span>safety · {(safety * 100).toFixed(0)}%</span>
          <span>Mark ${d.mark.toFixed(4)}</span>
        </div>
        <div style={{ position: 'relative', height: 6, background: 'var(--bg)', borderRadius: 999, overflow: 'hidden' }}>
          <div style={{
            position: 'absolute', left: 0, top: 0, bottom: 0,
            width: `${safety * 100}%`,
            background: safety > 0.6
              ? 'linear-gradient(90deg, var(--danger), var(--warning), var(--success))'
              : safety > 0.3 ? 'linear-gradient(90deg, var(--danger), var(--warning))'
              : 'var(--danger)',
            transition: 'width 400ms',
          }} />
          {/* mark indicator */}
          <div style={{
            position: 'absolute', left: `${safety * 100}%`, top: -3, width: 2, height: 12,
            background: '#fff', transform: 'translateX(-1px)', borderRadius: 999,
          }} />
        </div>
      </div>

      {/* metrics */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 8, paddingTop: 4 }}>
        <Metric label="Entry" val={`$${d.entry.toFixed(4)}`} />
        <Metric label="Funding" val={`${d.funding >= 0 ? '+' : ''}${(d.funding * 100).toFixed(3)}%`} c={d.funding >= 0 ? 'var(--success)' : 'var(--danger)'} />
        <Metric label="Margin" val={`$${fmt(d.size * d.entry / d.lev, 2)}`} />
      </div>

      {/* actions */}
      <div style={{ display: 'flex', gap: 6 }}>
        <button style={actBtn}>TP/SL</button>
        <button style={actBtn}>Add</button>
        <button style={{ ...actBtn, color: 'var(--danger)', borderColor: 'rgba(255,90,101,0.3)' }}>Close</button>
      </div>
    </div>
  );
}

function Metric({ label, val, c }) {
  return (
    <div>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.12em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>{label}</div>
      <div style={{ fontFamily: 'var(--font-mono)', fontSize: 12, color: c || 'var(--fg)', marginTop: 3, fontVariantNumeric: 'tabular-nums' }}>{val}</div>
    </div>
  );
}

const actBtn = {
  flex: 1, padding: '8px', borderRadius: 8,
  background: 'var(--bg)', color: 'var(--fg-muted)',
  border: '1px solid var(--border-soft)',
  fontFamily: 'var(--font-body)', fontSize: 12,
  cursor: 'pointer', transition: 'all 150ms',
};

export default PositionCard;
