'use client';

import { useEffect, useId, useState, type FormEvent } from 'react';
import { useAccount, useConnect } from 'wagmi';
import { Button, FormField, NoticeBar } from '@/ui/primitives';
import { PAXEER_CONNECTOR_ID, usePaxeerOptional, type SignInProvider } from '@/core/wallet-sdk/embedded_sdk/embedded';

export const PAXEER_RETURN_KEY = 'paxeer:return';

const OAUTH_PROVIDERS: ReadonlyArray<{ id: SignInProvider; label: string }> = [
  { id: 'google', label: 'Google' },
  { id: 'apple', label: 'Apple' },
  { id: 'twitter', label: 'X' },
  { id: 'github', label: 'GitHub' },
  { id: 'discord', label: 'Discord' },
];

/** Remember where the user was so /auth/callback can return them after the OAuth round-trip. */
export function rememberReturnPath(): void {
  try {
    const path = `${window.location.pathname}${window.location.search}`;
    window.sessionStorage.setItem(PAXEER_RETURN_KEY, path.startsWith('/auth/') ? '/' : path);
  } catch {
    /* storage unavailable: callback falls back to "/" */
  }
}

export function readReturnPath(): string {
  try {
    const stored = window.sessionStorage.getItem(PAXEER_RETURN_KEY);
    window.sessionStorage.removeItem(PAXEER_RETURN_KEY);
    return stored && stored.startsWith('/') && !stored.startsWith('//') ? stored : '/';
  } catch {
    return '/';
  }
}

/**
 * Connects wagmi to the Paxeer embedded connector once a Supabase session is
 * present and no other wallet is connected. Mounted inside the sign-in dialog
 * and the auth callback page, so a user returning from a magic link or OAuth
 * redirect lands in the normal "sign" step without choosing a wallet.
 */
export function useEmbeddedAutoConnect(enabled = true): { connecting: boolean; error: string | null } {
  const paxeer = usePaxeerOptional();
  const { connectors, connectAsync } = useConnect();
  const { isConnected, status } = useAccount();
  const [connecting, setConnecting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const connector = connectors.find(candidate => candidate.id === PAXEER_CONNECTOR_ID);
  const ready = Boolean(enabled && paxeer?.isAuthenticated && connector && !isConnected && status === 'disconnected');

  useEffect(() => {
    if (!ready || !connector) return;
    let alive = true;
    setConnecting(true);
    void connectAsync({ connector })
      .then(() => { if (alive) setError(null); })
      .catch((failure: unknown) => { if (alive) { console.error('[sign-in] embedded wallet connect failed:', failure); setError("Couldn't connect your wallet. Try again"); } })
      .finally(() => { if (alive) setConnecting(false); });
    return () => { alive = false; };
  }, [ready, connector, connectAsync]);

  return { connecting, error };
}

interface EmbeddedSignInProps {
  disabled?: boolean;
}

/**
 * Email and social sign-in for the Paxeer Embedded Wallet. Renders nothing
 * when the deployment has no Supabase configuration, so the dialog stays
 * wallet-only there without a code change.
 */
export default function EmbeddedSignIn({ disabled = false }: EmbeddedSignInProps) {
  const paxeer = usePaxeerOptional();
  const emailId = useId();
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);
  const [localError, setLocalError] = useState<string | null>(null);
  const [redirecting, setRedirecting] = useState<SignInProvider | null>(null);
  const auto = useEmbeddedAutoConnect(Boolean(paxeer?.wallet));

  if (!paxeer || !paxeer.wallet) return null;

  const busy = disabled || paxeer.authBusy || auto.connecting || redirecting !== null;

  const submitEmail = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (busy) return;
    const trimmed = email.trim();
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmed)) { setLocalError('Enter a valid email'); return; }
    setLocalError(null);
    rememberReturnPath();
    const result = await paxeer.signInWithEmail(trimmed);
    if (result.ok) setSent(true);
    else { if (result.error) console.error('[sign-in] email link failed:', result.error); setLocalError("Couldn't send the email. Try again"); }
  };

  const startOAuth = async (provider: SignInProvider) => {
    if (busy) return;
    setLocalError(null); setRedirecting(provider);
    rememberReturnPath();
    try { await paxeer.signInWithOAuth(provider); }
    catch (failure: unknown) { console.error('[sign-in] social sign-in failed:', failure); setLocalError("Couldn't start sign-in. Try again"); setRedirecting(null); }
  };

  if (paxeer.isAuthenticated) {
    return (
      <section aria-label="Embedded wallet" className="flex flex-col gap-2 rounded-md bg-surface-2 p-3">
        <p className="text-sm text-text">Signed in as {paxeer.user?.email ?? 'your Paxeer account'}.</p>
        <p className="text-xs text-text-muted">{auto.connecting ? 'Connecting your wallet...' : 'Your wallet is ready. Sign in below'}</p>
        {auto.error ? <NoticeBar tone="danger" surface={2}>{auto.error}</NoticeBar> : null}
        <Button variant="ghost" size="sm" disabled={busy} onClick={() => { void paxeer.signOut(); }}>Use a different account</Button>
      </section>
    );
  }

  return (
    <section aria-label="Email or social sign-in" className="flex flex-col gap-3 rounded-md bg-surface-2 p-3">
      <div className="flex flex-col gap-0.5">
        <h3 className="text-sm font-bold text-text">Continue with email or a social account</h3>
        <p className="text-xs text-text-muted">We make a wallet for you, no extension needed</p>
      </div>
      {sent ? (
        <NoticeBar tone="success" surface={2}>Check {email.trim()} for a sign-in link. Opening it finishes sign-in on this device.</NoticeBar>
      ) : (
        <form className="flex flex-col gap-2" onSubmit={event => { void submitEmail(event); }} noValidate>
          <FormField id={emailId} label="Email" surface="surface-2" error={localError}
            inputProps={{ type: 'email', autoComplete: 'email', inputMode: 'email', placeholder: 'you@example.com', value: email, disabled: busy, onChange: event => setEmail(event.target.value) }} />
          <Button type="submit" variant="secondary" size="md" fullWidth loading={paxeer.authBusy && redirecting === null} disabled={busy}>Send sign-in link</Button>
        </form>
      )}
      <div className="grid grid-cols-5 gap-2" role="group" aria-label="Social sign-in">
        {OAUTH_PROVIDERS.map(provider => (
          <Button key={provider.id} variant="ghost" size="md" loading={redirecting === provider.id} disabled={busy} onClick={() => { void startOAuth(provider.id); }} aria-label={`Continue with ${provider.label}`}>
            {provider.label}
          </Button>
        ))}
      </div>
      {paxeer.authError && !localError ? <NoticeBar tone="danger" surface={2}>{paxeer.authError}</NoticeBar> : null}
    </section>
  );
}
