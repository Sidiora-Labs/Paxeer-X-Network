'use client';

import { useEffect, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import { discoverProviders, type Eip6963ProviderDetail, type PaxeerWallet } from '@paxeer/wallet';
import { useSession, useStandardAccount } from '@paxeer/wallet/react';
import { getAuthRedirectUrl } from '@/lib/paxeer';
import { ProviderButton } from './ProviderButton';
import { SupportedWalletPanel } from './FundedAccountPanel';

type View = 'chooser' | 'auth' | 'embedded' | 'injected';

interface WalletModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  paxeer: PaxeerWallet;
  origin: string;
}

export function WalletModal({ open, onOpenChange, paxeer, origin }: WalletModalProps) {
  const { user, loading } = useSession(paxeer);
  const standard = useStandardAccount(paxeer);
  const [view, setView] = useState<View>('chooser');
  const [providers, setProviders] = useState<Eip6963ProviderDetail[]>([]);
  const [injected, setInjected] = useState<Eip6963ProviderDetail | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) { setView('chooser'); setInjected(null); return; }
    const discovery = discoverProviders(detail => setProviders(prior => prior.some(p => p.info.uuid === detail.info.uuid) ? prior : [...prior, detail]));
    setProviders([...discovery.providers]);
    return () => discovery.stop();
  }, [open]);

  useEffect(() => {
    if (view === 'auth' && user) setView('embedded');
    if (view === 'embedded' && !loading && !user) setView('auth');
  }, [user, loading, view]);

  async function provision() {
    setPending(true); setError(null);
    try { await paxeer.provisionStandardWallet(); await standard.refresh(); }
    catch (cause) { setError(cause instanceof Error ? cause.message : 'Provisioning unavailable'); }
    finally { setPending(false); }
  }

  return <Dialog.Root open={open} onOpenChange={onOpenChange}>
    <Dialog.Portal>
      <Dialog.Overlay className="fixed inset-0 z-40 bg-black/70" />
      <Dialog.Content className="fixed left-1/2 top-1/2 z-50 max-h-[90vh] w-[94vw] max-w-lg -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-2xl border border-neutral-700 bg-neutral-900 p-6">
        <Dialog.Title className="text-xl">Supported wallets</Dialog.Title>
        <Dialog.Description className="mb-4 text-sm text-neutral-400">Embedded threshold custody or your injected wallet. Every signing action requires approval.</Dialog.Description>
        <Dialog.Close aria-label="Close wallet" className="absolute right-4 top-4">×</Dialog.Close>
        {view !== 'chooser' && <button type="button" onClick={() => setView('chooser')} className="mb-4 text-sm">← Wallet choices</button>}
        {view === 'chooser' && <div className="flex flex-col gap-3">
          <button type="button" data-wallet-embedded onClick={() => setView(user ? 'embedded' : 'auth')} className="rounded-xl border border-neutral-700 p-3 text-left">Embedded wallet<br /><span className="text-sm text-neutral-400">Sign in and explicitly provision threshold custody.</span></button>
          {providers.map(detail => <button type="button" key={detail.info.uuid} data-wallet-injected onClick={() => { setInjected(detail); setView('injected'); }} className="rounded-xl border border-neutral-700 p-3 text-left">{detail.info.name}<br /><span className="text-sm text-neutral-400">Injected wallet · signing controlled by your wallet.</span></button>)}
          {!providers.length && <p role="status" className="text-sm text-neutral-400">No injected wallet announced. Enable an EIP-6963 wallet to connect.</p>}
        </div>}
        {view === 'auth' && <AuthView paxeer={paxeer} />}
        {view === 'embedded' && user && <>
          {standard.loading ? <p role="status">Loading embedded wallet…</p> : standard.error ? <p role="alert">{standard.error.message}</p> : !standard.data ? <button type="button" data-wallet-provision disabled={pending} onClick={provision} className="rounded-xl bg-[#004ced] p-3">{pending ? 'Creating…' : 'Create embedded threshold-custody wallet'}</button> : <SupportedWalletPanel paxeer={paxeer} origin={origin} />}
          {error && <p role="alert">{error}</p>}
          <button type="button" onClick={async () => { try { await paxeer.signOut(); setView('chooser'); } catch (cause) { setError(cause instanceof Error ? cause.message : 'Sign-out unavailable'); } }} className="mt-4 text-sm">Sign out</button>
        </>}
        {view === 'injected' && injected && <SupportedWalletPanel paxeer={paxeer} origin={origin} injected={injected} />}
      </Dialog.Content>
    </Dialog.Portal>
  </Dialog.Root>;
}

function AuthView({ paxeer }: { paxeer: PaxeerWallet }) {
  const [email, setEmail] = useState('');
  const [emailStatus, setEmailStatus] = useState<
    { kind: 'idle' } | { kind: 'sending' } | { kind: 'sent' } | { kind: 'error'; message: string }
  >({ kind: 'idle' });
  const [oauthLoading, setOauthLoading] = useState<string | null>(null);

  async function clickProvider(provider: 'google' | 'github' | 'twitter') {
    setOauthLoading(provider);
    try {
      const redirectTo = getAuthRedirectUrl();
      await paxeer.signInWithOAuth(provider, redirectTo);
      // Browser will redirect — we never reach this line.
    } catch (err) {
      setOauthLoading(null);
      setEmailStatus({
        kind: 'error',
        message: err instanceof Error ? err.message : 'sign-in failed',
      });
    }
  }

  async function submitEmail(e: React.FormEvent) {
    e.preventDefault();
    if (!email.includes('@')) return;
    setEmailStatus({ kind: 'sending' });
    try {
      const redirectTo = getAuthRedirectUrl();
      const { error } = await paxeer.signInWithEmail(email, redirectTo);
      setEmailStatus(error ? { kind: 'error', message: error.message } : { kind: 'sent' });
    } catch (cause) {
      setEmailStatus({ kind: 'error', message: cause instanceof Error ? cause.message : 'Sign-in unavailable' });
    }
  }

  if (emailStatus.kind === 'sent') {
    return (
      <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-[#004ced]/10 text-[#8FA8FF]">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
              d="M3 7l9 6 9-6M5 19h14a2 2 0 002-2V7a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z"
              stroke="currentColor"
              strokeWidth="1.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </div>
        <h3 className="text-[18px] text-neutral-100">Check your email</h3>
        <p className="text-[14px] text-neutral-400">
          We sent a magic link to <span className="text-neutral-100">{email}</span>.
          <br />
          Click it to finish signing in.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3 px-6 py-6">
      <p className="text-[14px] text-neutral-400">
        Sign in to explicitly create an embedded threshold-custody wallet.
      </p>

      <div className="mt-2 flex flex-col gap-2">
        <ProviderButton
          label="Continue with Google"
          iconSrc="/icons/google.svg"
          onClick={() => clickProvider('google')}
          loading={oauthLoading === 'google'}
          disabled={oauthLoading !== null}
        />
        <ProviderButton
          label="Continue with X"
          iconSrc="/icons/x.svg"
          onClick={() => clickProvider('twitter')}
          loading={oauthLoading === 'twitter'}
          disabled={oauthLoading !== null}
        />
        <ProviderButton
          label="Continue with GitHub"
          iconSrc="/icons/github.svg"
          onClick={() => clickProvider('github')}
          loading={oauthLoading === 'github'}
          disabled={oauthLoading !== null}
        />
      </div>

      <hr className="border-neutral-700" />

      <form onSubmit={submitEmail} className="flex flex-col gap-2">
        <label htmlFor="email" className="sr-only">
          Email
        </label>
        <input
          id="email"
          type="email"
          autoComplete="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          disabled={emailStatus.kind === 'sending'}
          className="
            w-full rounded-xl border border-neutral-700 bg-neutral-800
            px-3.5 py-3 text-[14px] text-neutral-100
            placeholder:text-neutral-600
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            focus:border-[#004ced] focus:outline-none
          "
        />
        <button
          type="submit"
          disabled={!email.includes('@') || emailStatus.kind === 'sending'}
          className="
            flex h-12 items-center justify-center rounded-xl
            bg-[#004ced] text-[15px] text-white
            transition-[background,opacity] duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:bg-[#0040c9]
            disabled:cursor-not-allowed disabled:opacity-40
          "
        >
          {emailStatus.kind === 'sending' ? 'Sending…' : 'Send magic link'}
        </button>
        {emailStatus.kind === 'error' && (
          <p className="mt-1 text-[12px] text-[#ff5a65]">{emailStatus.message}</p>
        )}
      </form>

      <p className="mt-2 text-center text-[12px] text-neutral-500">
        By continuing you agree to Paxeer&apos;s terms.
      </p>
    </div>
  );
}
