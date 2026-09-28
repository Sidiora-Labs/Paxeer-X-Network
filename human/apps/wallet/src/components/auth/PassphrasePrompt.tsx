'use client';

import { useState } from 'react';
import { Loader2 } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { useLocale } from '@/providers/LocaleProvider';

interface PassphrasePromptProps {
  title: string;
  subtitle: string;
  error?: string;
  onSubmit: (password: string) => Promise<void> | void;
  onCancel: () => void;
}

export function PassphrasePrompt({
  title,
  subtitle,
  error,
  onSubmit,
  onCancel,
}: PassphrasePromptProps) {
  const { p, t } = useLocale();
  const [pin, setPin] = useState('');
  const [busy, setBusy] = useState(false);

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    if (!/^\d{6}$/.test(pin) || busy) return;
    setBusy(true);
    try {
      await onSubmit(pin);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[200] flex items-center justify-center bg-pax-bg/95 px-6 backdrop-blur-sm">
      <form
        onSubmit={submit}
        className="w-full max-w-sm rounded-lg bg-pax-surface p-5"
      >
        <div className="flex items-start gap-3">
          <div className="w-10 h-10 rounded-lg bg-white/[0.08] flex items-center justify-center shrink-0">
            <SvgIcon name="lock" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.7)' }} />
          </div>
          <div>
            <h2 className="text-base font-bold">{title}</h2>
            <p className="text-xs text-pax-muted mt-1">{subtitle}</p>
          </div>
        </div>
        <label className="block text-xs font-medium text-pax-muted mt-5 mb-2">
          {p.pin}
        </label>
        <input
          autoFocus
          autoComplete="current-password"
          inputMode="numeric"
          pattern="[0-9]*"
          type="password"
          maxLength={6}
          value={pin}
          onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 6))}
          className="w-full h-11 rounded-lg bg-pax-bg px-3 text-center text-lg tracking-[0.35em] outline-none focus:bg-white/[0.04]"
        />
        {error && <p className="text-red-400 text-xs mt-3">{error}</p>}
        <div className="flex gap-3 mt-5">
          <button
            type="button"
            onClick={onCancel}
            className="h-11 flex-1 rounded-lg bg-white/[0.06] text-sm"
          >
            {t.common.cancel}
          </button>
          <button
            type="submit"
            disabled={pin.length !== 6 || busy}
            className="h-11 flex-1 rounded-lg bg-pax-accent text-black text-sm font-semibold disabled:opacity-60 flex items-center justify-center"
          >
            {busy ? <Loader2 className="w-4 h-4 animate-spin" /> : t.common.confirm}
          </button>
        </div>
      </form>
    </div>
  );
}
