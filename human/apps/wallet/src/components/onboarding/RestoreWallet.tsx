'use client';

import { useState } from 'react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { Loader2 } from 'lucide-react';

interface RestoreWalletProps {
  onRestore: (mnemonic: string) => Promise<void>;
  onBack: () => void;
}

export function RestoreWallet({ onRestore, onBack }: RestoreWalletProps) {
  const [words, setWords] = useState<string[]>(Array(12).fill(''));
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  const handleChange = (i: number, value: string) => {
    const next = [...words];
    next[i] = value.trim().toLowerCase();
    setWords(next);
  };

  const handlePaste = (e: React.ClipboardEvent) => {
    const text = e.clipboardData.getData('text').trim();
    const pasted = text.split(/\s+/);
    if (pasted.length === 12) {
      e.preventDefault();
      setWords(pasted.map((w) => w.toLowerCase()));
    }
  };

  const handleSubmit = async () => {
    const mnemonic = words.join(' ');
    if (words.some((w) => !w)) {
      setError('Please fill in all 12 words');
      return;
    }
    setLoading(true);
    setError('');
    try {
      await onRestore(mnemonic);
    } catch (e: any) {
      setError(e.message || 'Invalid recovery phrase');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex flex-col px-6 py-4">
      <button onClick={onBack} className="p-2 -ml-2 self-start press-scale">
        <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
      </button>

      <div className="flex-1 flex flex-col items-center mt-4">
        <h2 className="text-xl font-bold mb-1">Restore Wallet</h2>
        <p className="text-sm text-pax-muted text-center mb-6">
          Enter your 12-word recovery phrase
        </p>

        <div className="w-full max-w-sm grid grid-cols-3 gap-2 mb-4" onPaste={handlePaste}>
          {words.map((word, i) => (
            <div key={i} className="flex items-center gap-1 px-2 py-1.5 rounded-lg bg-white/5">
              <span className="text-[10px] text-pax-muted w-4 text-right">{i + 1}</span>
              <input
                type="text"
                value={word}
                onChange={(e) => handleChange(i, e.target.value)}
                className="flex-1 bg-transparent text-sm outline-none min-w-0"
                autoComplete="off"
                autoCapitalize="none"
              />
            </div>
          ))}
        </div>

        {error && <p className="text-red-400 text-xs mb-4">{error}</p>}
      </div>

      <button
        onClick={handleSubmit}
        disabled={loading}
        className="w-full max-w-sm mx-auto flex items-center justify-center gap-2 h-13 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale disabled:opacity-50"
        style={{ height: '52px' }}
      >
        {loading ? (
          <Loader2
            aria-label="Restoring wallet"
            className="h-5 w-5 animate-spin"
          />
        ) : (
          <>
            <SvgIcon name="rotate-180" className="w-4 h-4" style={{ filter: 'brightness(0) invert(0.6)' }} />
            Restore Wallet
          </>
        )}
      </button>
    </div>
  );
}
