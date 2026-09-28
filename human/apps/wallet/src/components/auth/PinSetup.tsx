'use client';

import { useState } from 'react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { useLocale } from '@/providers/LocaleProvider';

interface PinSetupProps {
  onComplete: (password: string) => void;
  onBack: () => void;
  title: string;
  subtitle: string;
}

export function PinSetup({ onComplete, onBack, title, subtitle }: PinSetupProps) {
  const { p } = useLocale();
  const [pin, setPin] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState('');

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    if (!/^\d{6}$/.test(pin)) {
      setError(p.enterPin);
      return;
    }
    if (pin !== confirmation) {
      setError(p.pinsMismatch);
      return;
    }
    setError('');
    onComplete(pin);
  };

  return (
    <div className="min-h-screen flex flex-col">
      <div className="p-4">
        <button onClick={onBack} className="p-2 -ml-2 press-scale" aria-label="Back">
          <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
        </button>
      </div>
      <form
        onSubmit={submit}
        className="flex-1 flex flex-col items-center justify-center px-6 pb-8"
      >
        <div className="w-full max-w-sm">
          <h2 className="text-xl font-bold mb-1">{title}</h2>
          <p className="text-sm text-pax-muted mb-7">{subtitle}</p>

          <label className="block text-xs font-medium text-pax-muted mb-2">
            {p.pin}
          </label>
          <input
            autoFocus
            autoComplete="new-password"
            inputMode="numeric"
            pattern="[0-9]*"
            type="password"
            maxLength={6}
            value={pin}
            onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 6))}
            className="w-full h-12 rounded-lg bg-white/[0.07] px-3 text-center text-xl tracking-[0.35em] outline-none focus:bg-white/[0.1]"
          />

          <label className="block text-xs font-medium text-pax-muted mt-4 mb-2">
            {p.confirmPin}
          </label>
          <input
            autoComplete="new-password"
            inputMode="numeric"
            pattern="[0-9]*"
            type="password"
            maxLength={6}
            value={confirmation}
            onChange={(event) => setConfirmation(event.target.value.replace(/\D/g, '').slice(0, 6))}
            className="w-full h-12 rounded-lg bg-white/[0.07] px-3 text-center text-xl tracking-[0.35em] outline-none focus:bg-white/[0.1]"
          />
          <p className="text-xs text-pax-muted mt-2">
            Use this PIN to unlock and approve sensitive wallet actions.
          </p>
          {error && <p className="text-red-400 text-xs mt-3">{error}</p>}

          <button
            type="submit"
            className="w-full h-12 rounded-lg bg-pax-accent text-black font-semibold text-sm mt-6"
          >
            Continue
          </button>
        </div>
      </form>
    </div>
  );
}
