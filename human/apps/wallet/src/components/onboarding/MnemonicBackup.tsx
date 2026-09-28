'use client';

import { useState } from 'react';
import { SvgIcon } from '@/components/ui/SvgIcon';

interface MnemonicBackupProps {
  mnemonic: string;
  onDone: () => void;
}

export function MnemonicBackup({ mnemonic, onDone }: MnemonicBackupProps) {
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState(false);
  const words = mnemonic.split(' ');

  const handleCopy = async () => {
    await navigator.clipboard.writeText(mnemonic);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="min-h-screen flex flex-col px-6 py-8">
      <div className="flex-1 flex flex-col items-center">
        <h2 className="text-xl font-bold mb-1">Back Up Your Phrase</h2>
        <p className="text-sm text-pax-muted text-center mb-6">
          Write down these 12 words in order. This is the only way to recover your wallet.
        </p>

        <div className="w-full max-w-sm glass-card p-4 mb-4 relative">
          {!revealed && (
            <div className="absolute inset-0 rounded-2xl bg-pax-bg/80 backdrop-blur-md flex flex-col items-center justify-center z-10 gap-2">
              <SvgIcon name="lock" className="w-6 h-6" style={{ filter: 'brightness(0) invert(0.6)' }} />
              <button
                onClick={() => setRevealed(true)}
                className="text-sm text-pax-accent font-medium press-scale"
              >
                Tap to reveal
              </button>
            </div>
          )}
          <div className="grid grid-cols-3 gap-2">
            {words.map((word, i) => (
              <div
                key={i}
                className="flex items-center gap-1.5 px-2 py-2 rounded-lg bg-white/5"
              >
                <span className="text-[10px] text-pax-muted w-4 text-right">{i + 1}</span>
                <span className="text-sm font-medium">{word}</span>
              </div>
            ))}
          </div>
        </div>

        {revealed && (
          <button
            onClick={handleCopy}
            className="flex items-center gap-2 text-sm text-pax-muted press-scale mb-4"
          >
            {copied ? <SvgIcon name="check" className="w-4 h-4" style={{ filter: 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }} /> : <SvgIcon name="copy" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} />}
            {copied ? 'Copied!' : 'Copy to clipboard'}
          </button>
        )}

        <div className="w-full max-w-sm p-3 rounded-xl bg-amber-500/10 mb-6">
          <p className="text-xs text-amber-300 text-center">
            Never share your recovery phrase. Anyone with it can access your funds.
          </p>
        </div>
      </div>

      <button
        onClick={onDone}
        className="w-full max-w-sm mx-auto flex items-center justify-center gap-2 h-13 rounded-2xl bg-pax-accent text-black font-semibold text-sm press-scale"
        style={{ height: '52px' }}
      >
        I've saved it
        <SvgIcon name="arrow-right" className="w-4 h-4" style={{ filter: 'brightness(0)' }} />
      </button>
    </div>
  );
}
