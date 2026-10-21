"use client";
import { useState } from "react";
import { TOKENS, Icon, fmt } from "./_shared";

// network-fee preview, and address / QR for deposit side.

export function TransferModal({ initialMode = 'deposit' } = {}) {
  const tokens = TOKENS;
  const [mode, setMode] = useState(initialMode);
  const [asset, setAsset] = useState(tokens[1]); // USDC
  const [amount, setAmount] = useState('500.00');
  const addr = '0x7a3f2E4cD8B1a9F02E7c4AaB3e09a82B9b2c';
  const fee = mode === 'deposit' ? 0 : 0.00042;

  return (
    <div style={{
      width: 380, background: 'var(--bg-elevated)', border: '1px solid var(--border-strong)',
      borderRadius: 18, boxShadow: 'var(--shadow-06)', overflow: 'hidden',
    }}>
      {/* head */}
      <div style={{ padding: '14px 18px', borderBottom: '1px solid var(--border-soft)', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
        <h3 style={{ margin: 0, fontSize: 14, fontWeight: 500 }}>Transfer</h3>
        <button style={{ background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)', borderRadius: 999, width: 24, height: 24, color: 'var(--fg-muted)', cursor: 'pointer', display: 'grid', placeItems: 'center' }}>
          <Icon.x />
        </button>
      </div>

      {/* tabs */}
      <div style={{ padding: 12, display: 'flex', gap: 4 }}>
        {['deposit', 'withdraw'].map(m => (
          <button key={m} onClick={() => setMode(m)} style={{
            flex: 1, padding: '10px', borderRadius: 10, border: 'none',
            background: mode === m ? 'var(--bg-subtle)' : 'transparent',
            color: mode === m ? 'var(--fg)' : 'var(--fg-muted)',
            fontFamily: 'var(--font-body)', fontSize: 13, fontWeight: 500,
            cursor: 'pointer', textTransform: 'capitalize',
            display: 'inline-flex', alignItems: 'center', justifyContent: 'center', gap: 8,
            border: mode === m ? '1px solid var(--border-soft)' : '1px solid transparent',
          }}>
            {m === 'deposit' ? <Icon.receive /> : <Icon.send />}
            {m}
          </button>
        ))}
      </div>

      {/* body */}
      <div style={{ padding: '0 18px 18px', display: 'flex', flexDirection: 'column', gap: 12 }}>
        {/* asset selector */}
        <div>
          <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)', marginBottom: 6 }}>Asset</div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
            {tokens.slice(0, 5).map(t => (
              <button key={t.sym} onClick={() => setAsset(t)} style={{
                display: 'inline-flex', alignItems: 'center', gap: 6,
                padding: '6px 10px 6px 6px', borderRadius: 999,
                background: asset.sym === t.sym ? 'var(--bg-subtle)' : 'transparent',
                border: `1px solid ${asset.sym === t.sym ? 'var(--border-strong)' : 'var(--border-soft)'}`,
                color: 'var(--fg)', fontFamily: 'var(--font-body)', fontSize: 12,
                cursor: 'pointer',
              }}>
                <span style={{ width: 18, height: 18, borderRadius: 999, background: t.color, display: 'grid', placeItems: 'center', color: '#fff', fontSize: 9, fontWeight: 600 }}>{t.sym[0]}</span>
                {t.sym}
              </button>
            ))}
          </div>
        </div>

        {mode === 'deposit' ? (
          <>
            <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Your deposit address</div>
            <div style={{
              display: 'grid', gridTemplateColumns: '96px 1fr', gap: 14,
              padding: 12, background: 'var(--bg-subtle)',
              border: '1px solid var(--border-soft)', borderRadius: 12,
            }}>
              <div style={{
                width: 96, height: 96, background: '#fff', borderRadius: 8, padding: 6, boxSizing: 'border-box',
                display: 'grid', gridTemplateColumns: 'repeat(9, 1fr)', gap: 1,
              }}>
                {Array.from({ length: 81 }).map((_, i) => (
                  <div key={i} style={{ background: (i * 19 + (i >> 2) * 5) % 3 === 0 ? 'var(--brand-black)' : '#fff', borderRadius: 1 }} />
                ))}
              </div>
              <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: 11, color: 'var(--fg)', wordBreak: 'break-all', lineHeight: 1.5 }}>
                  {addr}
                </div>
                <button style={{
                  alignSelf: 'flex-start',
                  padding: '4px 10px', borderRadius: 6,
                  background: 'var(--bg)', color: 'var(--color-blue-200)',
                  border: '1px solid rgba(0,76,237,0.3)',
                  fontFamily: 'var(--font-body)', fontSize: 11,
                  cursor: 'pointer', display: 'inline-flex', alignItems: 'center', gap: 4,
                }}><Icon.copy /> Copy address</button>
              </div>
            </div>
            <div style={{
              padding: '10px 12px', borderRadius: 10,
              background: 'rgba(255,158,44,0.1)', border: '1px solid rgba(255,158,44,0.25)',
              fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--warning)', letterSpacing: '0.04em', lineHeight: 1.5,
            }}>
              ⚠ Only send {asset.sym} on Paxeer Network (chain 125). Other chains will lose funds.
            </div>
          </>
        ) : (
          <>
            <div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)', marginBottom: 6 }}>Send to</div>
              <input placeholder="0x… or name.pax"
                style={{
                  width: '100%', padding: '10px 12px',
                  background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
                  borderRadius: 10, color: 'var(--fg)',
                  fontFamily: 'var(--font-mono)', fontSize: 13, outline: 'none',
                  boxSizing: 'border-box',
                }} />
            </div>
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: 9, letterSpacing: '0.14em', textTransform: 'uppercase', color: 'var(--fg-subtle)' }}>Amount</div>
                <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-muted)' }}>Balance · {fmt(asset.bal, 2)}</div>
              </div>
              <div style={{
                padding: 12, background: 'var(--bg-subtle)',
                border: '1px solid var(--border-soft)', borderRadius: 10,
                display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: 8,
              }}>
                <input value={amount} onChange={e => setAmount(e.target.value)} style={{
                  background: 'none', border: 'none', outline: 'none',
                  color: 'var(--fg)', fontFamily: "'Paxeer Grand Sans', Inter, sans-serif",
                  fontSize: 24, fontWeight: 400, letterSpacing: '-0.01em', width: '100%',
                }} />
                <span style={{ fontFamily: "'Paxeer Grand Sans', Inter, sans-serif", fontSize: 16, color: 'var(--fg-muted)' }}>{asset.sym}</span>
              </div>
              <div style={{ fontFamily: 'var(--font-mono)', fontSize: 10, color: 'var(--fg-subtle)', marginTop: 6 }}>
                ≈ ${fmt((parseFloat(amount) || 0) * asset.price, 2)}
              </div>
            </div>
            <div style={{
              padding: '10px 12px', borderRadius: 10,
              background: 'var(--bg-subtle)', border: '1px solid var(--border-soft)',
              display: 'flex', flexDirection: 'column', gap: 4,
            }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)' }}>
                <span>Network fee</span><span style={{ color: 'var(--fg)' }}>{fee.toFixed(5)} PAX · ~$0.00</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)' }}>
                <span>You send</span><span style={{ color: 'var(--fg)' }}>{amount} {asset.sym}</span>
              </div>
              <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 11, color: 'var(--fg-muted)', fontFamily: 'var(--font-mono)' }}>
                <span>Est. time</span><span style={{ color: 'var(--success)' }}>~4s · instant</span>
              </div>
            </div>
            <button style={{
              padding: '14px', borderRadius: 999, border: 'none',
              background: 'var(--accent-solid)', color: '#fff',
              fontFamily: 'var(--font-body)', fontSize: 14, fontWeight: 500,
              cursor: 'pointer', boxShadow: '0 8px 20px -8px rgba(0,76,237,0.5)',
            }}>
              Review withdraw
            </button>
          </>
        )}
      </div>
    </div>
  );
}

export default TransferModal;
