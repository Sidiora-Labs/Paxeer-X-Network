'use client';

import { useState } from 'react';
import { AlertTriangle, Fingerprint, Loader2 } from 'lucide-react';
import { WalletError } from '@/lib/wallet';
import {
  useWalletActions,
  useWalletState,
} from '@/providers/WalletProvider';
import { SvgIcon } from '@/components/ui/SvgIcon';
import {
  isBiometricUnlockEnrolled,
  recoverPinWithBiometrics,
} from '@/lib/biometric-unlock';
import { useLocale } from '@/providers/LocaleProvider';

function errorMessage(error: unknown): string {
  if (!(error instanceof WalletError)) {
    return error instanceof Error ? error.message : 'Unlock failed.';
  }
  switch (error.code) {
    case 'AUTHENTICATION_FAILED':
      return 'The PIN is incorrect.';
    case 'AUTHENTICATION_THROTTLED':
      return error.message;
    case 'MIGRATION_FAILED':
      return 'The legacy wallet could not be verified. Nothing was changed.';
    case 'CORRUPT_VAULT':
      return 'The wallet vault is corrupt or has been tampered with.';
    default:
      return error.message;
  }
}

export function PinLock() {
  const { p } = useLocale();
  const { migrationRequired, securityError } = useWalletState();
  const { unlock, migrateLegacy, migratePassphraseToPin } = useWalletActions();
  const [pin, setPin] = useState('');
  const [legacyPin, setLegacyPin] = useState('');
  const [legacyPassphrase, setLegacyPassphrase] = useState('');
  const [passphraseMigration, setPassphraseMigration] = useState(false);
  const [confirmation, setConfirmation] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [biometricLoading, setBiometricLoading] = useState(false);
  const [biometricEnrolled] = useState(isBiometricUnlockEnrolled);
  const stateError = (() => {
    switch (securityError) {
      case 'CORRUPT_VAULT':
        return 'The wallet vault is corrupt or has been tampered with.';
      case 'UNSUPPORTED_VERSION':
        return 'This wallet vault was created by an unsupported version.';
      case 'STORAGE_UNAVAILABLE':
        return 'Secure wallet storage is unavailable in this browser.';
      default:
        return '';
    }
  })();

  const submit = async (event: React.FormEvent) => {
    event.preventDefault();
    setError('');
    if (passphraseMigration) {
      if (!legacyPassphrase) {
        setError('Enter the passphrase used by the previous wallet version.');
        return;
      }
      if (!/^\d{6}$/.test(pin)) {
        setError('Enter a new 6-digit PIN.');
        return;
      }
      if (pin !== confirmation) {
        setError('PINs do not match.');
        return;
      }
    } else if (migrationRequired) {
      if (!/^\d{6}$/.test(legacyPin)) {
        setError('Enter the six-digit PIN used by the legacy wallet.');
        return;
      }
      if (!/^\d{6}$/.test(pin)) {
        setError('Enter a new 6-digit PIN.');
        return;
      }
      if (pin !== confirmation) {
        setError('PINs do not match.');
        return;
      }
    } else if (!/^\d{6}$/.test(pin)) {
      setError(p.enterPin);
      return;
    }

    setLoading(true);
    try {
      if (passphraseMigration) {
        await migratePassphraseToPin(legacyPassphrase, pin);
      } else if (migrationRequired) {
        await migrateLegacy(legacyPin, pin);
      } else {
        await unlock(pin);
      }
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setLoading(false);
    }
  };

  const unlockWithBiometrics = async () => {
    setError('');
    setBiometricLoading(true);
    try {
      const recoveredPin = await recoverPinWithBiometrics();
      await unlock(recoveredPin);
    } catch (caught) {
      setError(errorMessage(caught));
    } finally {
      setBiometricLoading(false);
    }
  };

  return (
    <div className="min-h-screen flex flex-col items-center justify-center px-6">
      <form onSubmit={submit} className="w-full max-w-sm">
        <div className="w-14 h-14 rounded-lg bg-white/[0.08] flex items-center justify-center mb-5">
          <SvgIcon
            name="lock"
            className="w-7 h-7"
            style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }}
          />
        </div>
        <h2 className="text-xl font-bold">
          {migrationRequired || passphraseMigration ? 'Secure Your Existing Wallet' : p.walletLocked}
        </h2>
        <p className="text-sm text-pax-muted mt-1 mb-6">
          {passphraseMigration
            ? 'Enter the previous credential once, then replace it with a 6-digit PIN.'
            : migrationRequired
            ? 'Verify the legacy wallet, then protect the new vault with a PIN.'
            : p.unlockInstruction}
        </p>

        {passphraseMigration && (
          <>
            <label className="block text-xs font-medium text-pax-muted mb-2">
              Previous wallet passphrase
            </label>
            <input
              autoFocus
              type="password"
              autoComplete="current-password"
              value={legacyPassphrase}
              onChange={(event) => setLegacyPassphrase(event.target.value)}
              className="w-full h-12 rounded-lg bg-white/[0.07] px-3 text-sm outline-none focus:bg-white/[0.1]"
            />
          </>
        )}

        {migrationRequired && !passphraseMigration && (
          <>
            <label className="block text-xs font-medium text-pax-muted mb-2">
              Legacy PIN
            </label>
            <input
              inputMode="numeric"
              autoComplete="off"
              maxLength={6}
              value={legacyPin}
              onChange={(event) =>
                setLegacyPin(event.target.value.replace(/\D/g, '').slice(0, 6))}
              className="w-full h-12 rounded-lg bg-white/[0.07] px-3 text-sm tracking-[0.3em] outline-none focus:bg-white/[0.1]"
            />
          </>
        )}

        <label className={`block text-xs font-medium text-pax-muted mb-2 ${migrationRequired || passphraseMigration ? 'mt-4' : ''}`}>
          {migrationRequired || passphraseMigration ? p.createPin : p.pin}
        </label>
        <input
          autoFocus={!migrationRequired && !passphraseMigration}
          autoComplete={migrationRequired || passphraseMigration ? 'new-password' : 'current-password'}
          inputMode="numeric"
          pattern="[0-9]*"
          type="password"
          maxLength={6}
          value={pin}
          onChange={(event) => setPin(event.target.value.replace(/\D/g, '').slice(0, 6))}
          className="w-full h-12 rounded-lg bg-white/[0.07] px-3 text-center text-xl tracking-[0.35em] outline-none focus:bg-white/[0.1]"
        />

        {(migrationRequired || passphraseMigration) && (
          <>
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
          </>
        )}

        {(error || stateError) && (
          <div className="flex items-start gap-2 mt-4 text-red-400">
            <AlertTriangle className="w-4 h-4 mt-0.5 shrink-0" />
            <p className="text-xs">{error || stateError}</p>
          </div>
        )}
        <button
          type="submit"
          disabled={loading}
          className="w-full h-12 rounded-lg bg-pax-accent text-black font-semibold text-sm mt-6 disabled:opacity-60 flex items-center justify-center"
        >
          {loading
            ? <Loader2 className="w-4 h-4 animate-spin" />
            : migrationRequired || passphraseMigration ? 'Verify and Migrate' : p.unlock}
        </button>
        {!migrationRequired && !passphraseMigration && biometricEnrolled && (
          <button
            type="button"
            onClick={unlockWithBiometrics}
            disabled={loading || biometricLoading}
            className="w-full h-12 rounded-lg bg-white/[0.08] text-sm font-semibold mt-3 disabled:opacity-60 flex items-center justify-center gap-2"
          >
            {biometricLoading
              ? <Loader2 className="w-4 h-4 animate-spin" />
              : <Fingerprint className="w-4 h-4" />}
            {p.unlockBiometrics}
          </button>
        )}
        {!migrationRequired && !passphraseMigration && (
          <button
            type="button"
            onClick={() => {
              setError('');
              setPin('');
              setConfirmation('');
              setPassphraseMigration(true);
            }}
            className="w-full h-11 text-xs text-pax-muted mt-2"
          >
            Migrate a previous wallet passphrase
          </button>
        )}
        {passphraseMigration && (
          <button
            type="button"
            onClick={() => {
              setError('');
              setLegacyPassphrase('');
              setPin('');
              setConfirmation('');
              setPassphraseMigration(false);
            }}
            className="w-full h-11 text-xs text-pax-muted mt-2"
          >
            Back to PIN unlock
          </button>
        )}
      </form>
    </div>
  );
}
