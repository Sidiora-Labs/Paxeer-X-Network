"use client";
import { useState } from "react";
import { TOKENS, fmt } from "./_shared";

// intelligent USD↔token toggle. Useful across any financial app.

export function DataInput() {
  const [amount, setAmount] = useState('500');
  const [mode, setMode] = useState('token'); // 'token' | 'usd'
  const token = TOKENS[0]; // PAX
  const bal = token.bal;

  const val = parseFloat(amount) || 0;
  const alt = mode === 'token' ? val * token.price : val / token.price;
  const pctOfBal = (mode === 'token' ? val : val / token.price) / bal;

  return (
    <div style={{ width: 360, display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{
        background: 'var(--bg-subtle)', border: '1px solid var(--border-strong)',
        borderRadius: 16, padding: 18,
        display: 'flex', flexDirection: 'column', gap: 10,
      }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span style={{ fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>
            Amount · {mode === 'token' ? token.sym : 'USD'}
          </span>
          <button onClick={() => setMode(m => m === 'token' ? 'usd' : 'token')}
            style={{
              fontFamily: 'var(--font-mono)', fontSize: 10, letterSpacing: '0.06em',
              padding: '3px 8px', borderRadius: 999,
              background: 'var(--bg)', color: 'var(--color-blue-200)',
              border: '1px solid rgba(0,76,237,0.3)', cursor: 'pointer',
              display: 'inline-flex', alignItems: 'center', gap: 4,
            }}>
            ⇌ switch to {mode === 'token' ? 'USD' : token.sym}
          </button>
        </div>

        <div style={{ display: 'flex', alignItems: 'baseline', gap: 10 }}>
          <span style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 42, color: 'var(--fg-subtle)' }}>
            {mode === 'usd' ? '$' : ''}
          </span>
          <input
            value={amount}
            onChange={e => setAmount(e.target.value)}
            style={{
              flex: 1, background: 'none', border: 'none', outline: 'none',
              fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
              fontSize: 42, fontWeight: 400, color: 'var(--fg)',
              letterSpacing: '-0.02em', width: '100%',
              fontVariantNumeric: 'tabular-nums',
            }}
          />
          {mode === 'token' && (
            <span style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 24, color: 'var(--fg-muted)' }}>
              {token.sym}
            </span>
          )}
        </div>

        <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-subtle)' }}>
          ≈ {mode === 'token' ? '$' + fmt(alt, 2) : fmt(alt, 4) + ' ' + token.sym}
        </div>

        {/* bal + quick pct */}
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', paddingTop: 10, borderTop: '1px solid var(--border-soft)' }}>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg-muted)' }}>
            Balance · {fmt(bal, 2)} {token.sym}
          </div>
          <div style={{ display: 'flex', gap: 4 }}>
            {[25, 50, 75, 100].map(p => (
              <button key={p} onClick={() => {
                const amt = mode === 'token' ? bal * p / 100 : bal * token.price * p / 100;
                setAmount(amt.toFixed(2));
              }}
                style={{
                  fontFamily: 'var(--font-mono)', fontSize: 10,
                  padding: '4px 8px', borderRadius: 6,
                  background: 'var(--bg)', color: 'var(--fg-muted)',
                  border: '1px solid var(--border-soft)', cursor: 'pointer',
                }}>
                {p === 100 ? 'MAX' : p + '%'}
              </button>
            ))}
          </div>
        </div>

        {/* balance bar */}
        <div style={{ height: 4, background: 'var(--bg)', borderRadius: 999, overflow: 'hidden' }}>
          <div style={{
            width: `${Math.min(100, pctOfBal * 100)}%`,
            height: '100%',
            background: pctOfBal > 1 ? 'var(--danger)' : 'linear-gradient(90deg, var(--accent-solid), #8FA8FF)',
            transition: 'width 200ms',
          }} />
        </div>
      </div>
    </div>
  );
}

export default DataInput;
