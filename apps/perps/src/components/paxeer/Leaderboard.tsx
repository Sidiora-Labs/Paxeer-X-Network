"use client";
import { Fragment } from "react";

export function Leaderboard() {
  const rows = [
    { rank: 1, addr: '0x7a3f2E4c…9b2c', pnl:  84213.44, trades: 482, wr: 74.1, hold: '2h 14m', vol: 1240000, me: false },
    { rank: 2, addr: 'pax.trader.eth',   pnl:  52482.10, trades: 318, wr: 68.5, hold: '4h 02m', vol:  982400, me: false },
    { rank: 3, addr: '0x8c2a1B98…4f19', pnl:  38244.80, trades: 641, wr: 62.8, hold: '48m',    vol: 1810000, me: false },
    { rank: 4, addr: '0xbb9122AA…0a4e', pnl:  29120.50, trades: 204, wr: 71.2, hold: '3h 40m', vol:  684000, me: false },
    { rank: 5, addr: '0x5e11C702…78ab', pnl:  24845.62, trades: 412, wr: 59.4, hold: '1h 22m', vol:  920000, me: false },
    { rank: 6, addr: 'degen.pax',         pnl:  18402.18, trades: 892, wr: 54.1, hold: '14m',    vol: 2400000, me: false },
    { rank: 7, addr: '0x22d8Af01…c031', pnl:  14820.04, trades: 148, wr: 66.2, hold: '5h 12m', vol:  384000, me: false },
    { rank: 42, addr: '0x9f04e…7b21',    pnl:   1245.82, trades: 142, wr: 58.4, hold: '2h 48m', vol:   12480, me: true },
  ];
  const fmtV = v => v >= 1_000_000 ? `$${(v/1_000_000).toFixed(2)}M` : v >= 1_000 ? `$${(v/1_000).toFixed(1)}K` : `$${v}`;

  return (
    <div style={{ width: '100%', maxWidth: 760, background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 14 }}>
      <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-soft)', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
        <div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Leaderboard · P&L ranked</div>
          <div style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 16, fontWeight: 400, color: 'var(--fg)', marginTop: 2 }}>7-day leaders</div>
        </div>
        <div style={{ display: 'flex', gap: 2, padding: 3, background: 'var(--bg)', border: '1px solid var(--border-soft)', borderRadius: 8 }}>
          {['24H','7D','30D','ALL'].map((r, i) => (
            <button key={r} style={{
              padding: '5px 10px', border: 'none', borderRadius: 5,
              background: i === 1 ? 'var(--bg-subtle)' : 'transparent',
              color: i === 1 ? 'var(--fg)' : 'var(--fg-muted)',
              fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.04em', cursor: 'pointer',
            }}>{r}</button>
          ))}
        </div>
      </div>
      <table style={{ width: '100%', borderCollapse: 'collapse', fontFamily: 'var(--font-mono)', fontSize: 11 }}>
        <thead>
          <tr style={{ color: 'var(--fg-subtle)', fontSize: 9, letterSpacing: '0.1em' }}>
            <th style={{ textAlign: 'left',  padding: '10px 14px 8px 18px', fontWeight: 400, width: 42 }}>#</th>
            <th style={{ textAlign: 'left',  padding: '10px 0 8px',   fontWeight: 400 }}>ADDRESS</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px',   fontWeight: 400 }}>P&amp;L</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px',   fontWeight: 400 }}>TRADES</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px',   fontWeight: 400 }}>WIN RATE</th>
            <th style={{ textAlign: 'right', padding: '10px 0 8px',   fontWeight: 400 }}>AVG HOLD</th>
            <th style={{ textAlign: 'right', padding: '10px 18px 8px', fontWeight: 400 }}>VOLUME</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => {
            const rankColor = r.rank === 1 ? '#FFB833' : r.rank === 2 ? '#C0CEE3' : r.rank === 3 ? '#CC8A5C' : 'var(--fg-muted)';
            const showDivider = r.me && !rows[i-1]?.me;
            return (
              <Fragment key={r.rank}>
                {showDivider && (
                  <tr><td colSpan="7" style={{ padding: '6px 18px', borderTop: '1px solid rgba(255,255,255,0.04)' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 10, fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.1em', color: 'var(--fg-subtle)' }}>
                      <div style={{ flex: 1, height: 1, background: 'rgba(255,255,255,0.05)' }} />
                      <span>YOU</span>
                      <div style={{ flex: 1, height: 1, background: 'rgba(255,255,255,0.05)' }} />
                    </div>
                  </td></tr>
                )}
                <tr style={{
                  borderTop: '1px solid rgba(255,255,255,0.04)',
                  color: 'var(--fg)',
                  background: r.me ? 'rgba(0,76,237,0.06)' : 'transparent',
                }}>
                  <td style={{ padding: '11px 14px 11px 18px' }}>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                      {r.rank <= 3 && (
                        <svg width="10" height="12" viewBox="0 0 10 12" fill="none"><path d="M1 0h8l-1 8H2L1 0zM3 9h4l1 3H2l1-3z" fill={rankColor} /></svg>
                      )}
                      <span style={{ color: rankColor, fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 13 }}>{r.rank}</span>
                    </div>
                  </td>
                  <td>
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                      <div style={{
                        width: 22, height: 22, borderRadius: 6,
                        background: `linear-gradient(135deg, ${stringColor(r.addr)}, ${stringColor(r.addr, 60)})`,
                      }} />
                      <span style={{ color: r.me ? 'var(--color-blue-200)' : 'var(--fg)' }}>{r.addr}</span>
                      {r.me && <span style={{ fontSize: 9, padding: '1px 5px', borderRadius: 3, background: 'rgba(0,76,237,0.2)', color: 'var(--color-blue-200)', letterSpacing: '0.06em' }}>YOU</span>}
                    </div>
                  </td>
                  <td style={{ textAlign: 'right', color: 'var(--success)' }}>+${r.pnl.toLocaleString('en-US',{minimumFractionDigits:2,maximumFractionDigits:2})}</td>
                  <td style={{ textAlign: 'right' }}>{r.trades}</td>
                  <td style={{ textAlign: 'right' }}>
                    <div style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
                      <div style={{ width: 36, height: 3, background: 'rgba(255,255,255,0.06)', borderRadius: 2, overflow: 'hidden' }}>
                        <div style={{ width: `${r.wr}%`, height: '100%', background: r.wr > 60 ? 'var(--success)' : 'var(--warning)' }} />
                      </div>
                      <span>{r.wr.toFixed(1)}%</span>
                    </div>
                  </td>
                  <td style={{ textAlign: 'right', color: 'var(--fg-muted)' }}>{r.hold}</td>
                  <td style={{ textAlign: 'right', padding: '11px 18px' }}>{fmtV(r.vol)}</td>
                </tr>
              </Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// deterministic pseudo-color from a string (for avatar gradient)
function stringColor(s, offset = 0) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0;
  return `hsl(${(Math.abs(h) + offset) % 360}, 70%, 55%)`;
}

export default Leaderboard;
