'use client';

import { useEffect, useMemo, useState } from 'react';
import * as Dialog from '@radix-ui/react-dialog';
import type { PaxeerWallet, FundedSelfResponse, PublicWallet } from '@paxeer/wallet';
import { PaxeerWalletError } from '@paxeer/wallet';
import {
  useFundedAccount,
  useSession,
  useStandardAccount,
} from '@paxeer/wallet/react';
import { paxeerWallet, getAuthRedirectUrl } from '@/lib/paxeer';
import { truncateAddress } from '@/lib/format';
import { ProviderButton } from './ProviderButton';
import { SendTxForm } from './SendTxForm';
import { FundedAccountPanel } from './FundedAccountPanel';

/**
 * Modal flow (post operator feedback): no surface auto-provisions a wallet on
 * the user's behalf. After sign-in we land on a chooser screen where the user
 * explicitly picks Standard or Funded. Either kind can be created without the
 * other ever existing.
 */
type View = 'auth' | 'chooser' | 'standard' | 'send' | 'funded';

interface WalletModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

/**
 * The drop-in Paxeer wallet modal.
 *
 * State machine:
 *   - signed out                       -> view: 'auth'
 *   - signed in (default landing)      -> view: 'chooser' (pick standard or funded)
 *   - chose Standard                   -> view: 'standard' (address card + Send + Funded entry)
 *   - chose Funded                     -> view: 'funded'   (FundedAccountPanel)
 *   - send button pressed              -> view: 'send'
 *
 * No surface auto-provisions a wallet. The standard wallet is created only when
 * the user clicks "Create standard wallet" inside the chooser; the funded
 * account is created only when the user clicks "Provision" inside the funded
 * panel.
 */
export function WalletModal({ open, onOpenChange }: WalletModalProps) {
  const paxeer = useMemo(() => paxeerWallet(), []);
  const { user, loading: sessionLoading } = useSession(paxeer);
  const standardQuery = useStandardAccount(paxeer);
  const fundedQuery = useFundedAccount(paxeer);
  const [view, setView] = useState<View>('auth');

  // Reset view when the user signs out / dialog closes.
  useEffect(() => {
    if (!open) return;
    if (!user) setView('auth');
    else if (view === 'auth') setView('chooser');
  }, [open, user, view]);

  return (
    <Dialog.Root open={open} onOpenChange={onOpenChange}>
      <Dialog.Portal>
        <Dialog.Overlay
          className="
            fixed inset-0 z-40 bg-black/70 backdrop-blur-sm
            overlay-fade
          "
        />
        <Dialog.Content
          className="
            fixed left-1/2 top-1/2 z-50 w-[92vw] max-w-[420px]
            -translate-x-1/2 -translate-y-1/2
            overflow-hidden rounded-2xl
            border border-neutral-700 bg-neutral-800
            shadow-[0_24px_48px_rgba(0,0,0,0.24)]
            panel-rise
            focus:outline-none
          "
        >
          <header className="flex items-center justify-between border-b border-neutral-700 px-6 py-4">
            <Dialog.Title className="text-[15px] text-neutral-100">
              {view === 'auth' && 'Connect to Paxeer'}
              {view === 'chooser' && 'Paxeer Wallet'}
              {view === 'standard' && 'Embedded Wallet'}
              {view === 'send' && 'Send'}
              {view === 'funded' && 'Funded Account'}
            </Dialog.Title>
            <Dialog.Close
              aria-label="Close"
              className="
                -mr-2 flex h-8 w-8 items-center justify-center rounded
                text-neutral-400
                transition-colors duration-[var(--duration-snappy)]
                ease-[var(--ease-standard)]
                hover:bg-neutral-700 hover:text-neutral-100
              "
            >
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
                <path
                  d="M6 6l12 12M18 6L6 18"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                />
              </svg>
            </Dialog.Close>
          </header>

          {sessionLoading ? (
            <Loading />
          ) : view === 'auth' ? (
            <AuthView />
          ) : view === 'send' ? (
            <SendTxForm
              paxeer={paxeer}
              explorerUrl={standardQuery.data?.chain.explorer_url ?? null}
              onBack={() => {
                setView('standard');
                standardQuery.refresh();
              }}
            />
          ) : view === 'funded' ? (
            <FundedAccountPanel
              paxeer={paxeer}
              explorerUrl={standardQuery.data?.chain.explorer_url ?? null}
              onBack={() => setView('chooser')}
            />
          ) : view === 'standard' ? (
            <StandardWalletView
              paxeer={paxeer}
              wallet={standardQuery.data?.wallet ?? null}
              loading={standardQuery.loading}
              error={standardQuery.error}
              onCreated={standardQuery.refresh}
              onBack={() => setView('chooser')}
              onSend={() => setView('send')}
              onSignOut={async () => {
                await paxeer.signOut();
                setView('auth');
              }}
            />
          ) : (
            // 'chooser' view — the default landing after sign-in.
            <ChooserView
              standard={standardQuery.data?.wallet ?? null}
              funded={fundedQuery.data}
              loading={standardQuery.loading || fundedQuery.loading}
              onPickStandard={() => setView('standard')}
              onPickFunded={() => setView('funded')}
              onSignOut={async () => {
                await paxeer.signOut();
                setView('auth');
              }}
            />
          )}
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

/* -------------------------------------------------------------------------- */
/* Auth view                                                                   */
/* -------------------------------------------------------------------------- */

function AuthView() {
  const paxeer = paxeerWallet();
  const [email, setEmail] = useState('');
  const [emailStatus, setEmailStatus] = useState<
    { kind: 'idle' } | { kind: 'sending' } | { kind: 'sent' } | { kind: 'error'; message: string }
  >({ kind: 'idle' });
  const [oauthLoading, setOauthLoading] = useState<string | null>(null);

  async function clickProvider(provider: 'google' | 'github' | 'twitter') {
    setOauthLoading(provider);
    try {
      const redirectTo = getAuthRedirectUrl();
      // eslint-disable-next-line no-console
      console.debug('[paxeer] OAuth redirectTo:', redirectTo);
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
    const redirectTo = getAuthRedirectUrl();
    // eslint-disable-next-line no-console
    console.debug('[paxeer] magic-link redirectTo:', redirectTo);
    const { error } = await paxeer.signInWithEmail(email, redirectTo);
    setEmailStatus(error ? { kind: 'error', message: error.message } : { kind: 'sent' });
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
        No seed phrase. No browser extension. Sign in to get a wallet on HyperPaxeer in seconds.
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

      <Divider />

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

/* -------------------------------------------------------------------------- */
/* Chooser — landing screen after sign-in                                       */
/* -------------------------------------------------------------------------- */

/**
 * After sign-in we show two clearly separate choices: a standard self-custody
 * wallet OR a funded prop-firm account. NEITHER is created until the user
 * clicks into it. The user can have one, the other, both, or (briefly) neither.
 */
function ChooserView({
  standard,
  funded,
  loading,
  onPickStandard,
  onPickFunded,
  onSignOut,
}: {
  standard: PublicWallet | null;
  funded: FundedSelfResponse | null;
  loading: boolean;
  onPickStandard: () => void;
  onPickFunded: () => void;
  onSignOut: () => void;
}) {
  return (
    <div className="flex flex-col gap-3 px-6 py-6">
      <p className="text-[13px] leading-[1.55] text-neutral-400">
        Pick a wallet. Both live on HyperPaxeer (chain 125). Standard is your own
        self-custody EOA; Funded is a separately-keyed prop-firm account
        pre-loaded with $25K USDL of trading capital.
      </p>

      <ChoiceCard
        title="Embedded Wallet"
        subtitle={
          standard
            ? `Connected · ${truncateAddress(standard.address)}`
            : 'Self-custody EOA · same address across every Paxeer app'
        }
        cta={standard ? 'Open' : 'Create'}
        tone={standard ? 'neutral' : 'primary'}
        loading={loading && !standard && !funded}
        onClick={onPickStandard}
      />

      <ChoiceCard
        title="Funded Account"
        subtitle={
          funded
            ? fundedSubtitle(funded)
            : '$25,000 USDL collateral · 15 PAX gas · drawdown rules enforced server-side'
        }
        cta={funded ? 'Open' : 'Create'}
        tone={funded ? 'neutral' : 'primary'}
        loading={loading && !standard && !funded}
        onClick={onPickFunded}
        accent="funded"
      />

      <div className="mt-3 flex justify-between border-t border-neutral-700 pt-4 text-[13px]">
        <span className="text-neutral-500">Signed in via Supabase.</span>
        <button
          type="button"
          onClick={onSignOut}
          className="
            text-neutral-400
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:text-[#ff5a65]
          "
        >
          Sign out
        </button>
      </div>
    </div>
  );
}

function fundedSubtitle(f: FundedSelfResponse): string {
  const a = f.funded_account;
  const equity = a.current_value_usd ?? a.starting_value_usd;
  const pretty = `$${parseFloat(equity).toLocaleString('en-US', { maximumFractionDigits: 0 })}`;
  return `${pretty} · ${a.status}`;
}

function ChoiceCard({
  title,
  subtitle,
  cta,
  tone,
  loading,
  onClick,
  accent,
}: {
  title: string;
  subtitle: string;
  cta: string;
  tone: 'primary' | 'neutral';
  loading: boolean;
  onClick: () => void;
  accent?: 'funded';
}) {
  const accentBorder =
    accent === 'funded' ? 'border-[#004ced]/30 bg-[#004ced]/5' : 'border-neutral-700 bg-neutral-800';
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={loading}
      className={`
        flex w-full items-center justify-between gap-3 rounded-xl
        ${accentBorder}
        border px-4 py-4 text-left
        transition-colors duration-[var(--duration-snappy)]
        ease-[var(--ease-standard)]
        hover:border-neutral-600 hover:bg-neutral-700
        disabled:cursor-not-allowed disabled:opacity-60
      `}
    >
      <div className="min-w-0 flex-1">
        <p className="text-[14px] text-neutral-100">{title}</p>
        <p className="mt-0.5 truncate text-[12px] leading-[1.45] text-neutral-400">{subtitle}</p>
      </div>
      <span
        className={`
          shrink-0 rounded-full px-3 py-1 text-[12px]
          ${
            tone === 'primary'
              ? 'bg-[#004ced] text-white'
              : 'border border-neutral-700 bg-neutral-800 text-neutral-100'
          }
        `}
      >
        {loading ? '…' : cta}
      </span>
    </button>
  );
}

/* -------------------------------------------------------------------------- */
/* Standard wallet view                                                        */
/* -------------------------------------------------------------------------- */

/**
 * Detail view for the standard self-custody wallet. If the user landed here
 * without an existing wallet (clicked "Create" in the chooser), we show a
 * clear creation CTA — we never silently provision in the background.
 */
function StandardWalletView({
  paxeer,
  wallet,
  loading,
  error,
  onCreated,
  onBack,
  onSend,
  onSignOut,
}: {
  paxeer: PaxeerWallet;
  wallet: PublicWallet | null;
  loading: boolean;
  error: Error | null;
  onCreated: () => void;
  onBack: () => void;
  onSend: () => void;
  onSignOut: () => void;
}) {
  const [copied, setCopied] = useState(false);
  const [provisionStatus, setProvisionStatus] = useState<
    | { kind: 'idle' }
    | { kind: 'creating' }
    | { kind: 'error'; message: string }
  >({ kind: 'idle' });

  async function copy() {
    if (!wallet) return;
    try {
      await navigator.clipboard.writeText(wallet.address);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* ignore */
    }
  }

  async function create() {
    setProvisionStatus({ kind: 'creating' });
    try {
      await paxeer.provisionStandardWallet();
      onCreated();
      setProvisionStatus({ kind: 'idle' });
    } catch (err) {
      setProvisionStatus({
        kind: 'error',
        message:
          err instanceof PaxeerWalletError || err instanceof Error
            ? err.message
            : 'failed to create wallet',
      });
    }
  }

  if (loading && !wallet) {
    return (
      <div className="flex flex-col gap-3 px-6 py-6">
        <BackHeader onBack={onBack} />
        <Loading label="Loading wallet…" />
      </div>
    );
  }

  if (error && !wallet) {
    return (
      <div className="flex flex-col gap-3 px-6 py-6">
        <BackHeader onBack={onBack} />
        <ErrorView message={error.message} onRetry={onCreated} />
      </div>
    );
  }

  // No wallet yet — explicit creation CTA.
  if (!wallet) {
    return (
      <div className="flex flex-col gap-4 px-6 py-6">
        <BackHeader onBack={onBack} />
        <div className="rounded-xl border border-neutral-700 bg-neutral-800 px-4 py-4">
          <p className="text-[12px] uppercase tracking-wider text-neutral-500">
            New embedded wallet
          </p>
          <h3 className="mt-1.5 text-[16px] text-neutral-100">
            Create your self-custody wallet
          </h3>
          <p className="mt-1.5 text-[12px] leading-[1.55] text-neutral-400">
            One EVM address tied to your Supabase identity, persistent across
            every Paxeer app. The private key is encrypted on the server with
            envelope encryption (AES-256-GCM).
          </p>
        </div>

        {provisionStatus.kind === 'error' && (
          <div className="rounded-lg border border-[#ff5a65]/30 bg-[#ff5a65]/5 px-3 py-2 text-[13px] text-[#ff5a65]">
            {provisionStatus.message}
          </div>
        )}

        <button
          type="button"
          onClick={create}
          disabled={provisionStatus.kind === 'creating'}
          className="
            flex h-12 items-center justify-center rounded-xl
            bg-[#004ced] text-[15px] text-white
            transition-[background,opacity] duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:bg-[#0040c9]
            disabled:cursor-not-allowed disabled:opacity-60
          "
        >
          {provisionStatus.kind === 'creating' ? 'Creating…' : 'Create Embedded Wallet'}
        </button>
      </div>
    );
  }

  // Wallet exists — show the standard address card.
  return (
    <div className="flex flex-col gap-4 px-6 py-5">
      <BackHeader onBack={onBack} />
      <div className="rounded-xl border border-neutral-700 bg-[#050505] px-4 py-4">
        <p className="text-[12px] uppercase tracking-wider text-neutral-500">Address</p>
        <button
          type="button"
          onClick={copy}
          className="
            mt-1 flex w-full items-center justify-between gap-3
            rounded font-mono text-[13px] text-neutral-100
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:text-[#8FA8FF]
          "
          aria-label="Copy wallet address"
        >
          <span className="truncate">{wallet.address}</span>
          {copied ? <CheckIcon /> : <CopyIcon />}
        </button>
        <div className="mt-3 flex items-center gap-2 text-[12px] text-neutral-400">
          <span className="inline-block h-1.5 w-1.5 rounded-full bg-[#05c168]" />
          <span>HyperPaxeer</span>
          <span className="text-neutral-600">·</span>
          <span className="font-mono">chain {wallet.chain_id}</span>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <button
          type="button"
          onClick={onSend}
          className="
            flex h-11 items-center justify-center gap-2 rounded-xl
            bg-[#004ced] text-[14px] text-white
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:bg-[#0040c9]
          "
        >
          <ArrowUpIcon />
          Send
        </button>
        <button
          type="button"
          onClick={copy}
          className="
            flex h-11 items-center justify-center gap-2 rounded-xl
            border border-neutral-700 bg-neutral-800
            text-[14px] text-neutral-100
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:border-neutral-600 hover:bg-neutral-700
          "
        >
          <ArrowDownIcon />
          Receive
        </button>
      </div>

      <div className="mt-2 flex justify-between border-t border-neutral-700 pt-4 text-[13px]">
        <span className="text-neutral-500">Same wallet across every Paxeer app.</span>
        <button
          type="button"
          onClick={onSignOut}
          className="
            text-neutral-400
            transition-colors duration-[var(--duration-snappy)]
            ease-[var(--ease-standard)]
            hover:text-[#ff5a65]
          "
        >
          Sign out
        </button>
      </div>
    </div>
  );
}

function BackHeader({ onBack }: { onBack: () => void }) {
  return (
    <button
      type="button"
      onClick={onBack}
      className="
        -ml-1 flex w-fit items-center gap-1 rounded px-1 py-0.5
        text-[13px] text-neutral-400
        transition-colors duration-[var(--duration-snappy)]
        ease-[var(--ease-standard)]
        hover:text-neutral-100
      "
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
        <path
          d="M15 18l-6-6 6-6"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        />
      </svg>
      Back
    </button>
  );
}

/* -------------------------------------------------------------------------- */
/* Misc helpers                                                                */
/* -------------------------------------------------------------------------- */

function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-12 text-center">
      <svg
        className="h-6 w-6 animate-spin text-neutral-500"
        viewBox="0 0 24 24"
        fill="none"
        aria-hidden="true"
      >
        <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="3" opacity="0.25" />
        <path fill="currentColor" d="M4 12a8 8 0 018-8v3a5 5 0 00-5 5H4z" />
      </svg>
      <p className="text-[13px] text-neutral-400">{label}</p>
    </div>
  );
}

function ErrorView({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="flex flex-col items-center gap-3 px-6 py-10 text-center">
      <p className="text-[15px] text-neutral-100">Something went wrong</p>
      <p className="text-[13px] text-[#ff5a65]">{message}</p>
      <button
        type="button"
        onClick={onRetry}
        className="
          mt-2 rounded-xl border border-neutral-700 bg-neutral-800
          px-4 py-2 text-[13px] text-neutral-100
          hover:border-neutral-600 hover:bg-neutral-700
        "
      >
        Try again
      </button>
    </div>
  );
}

function Divider() {
  return (
    <div className="my-2 flex items-center gap-3">
      <div className="h-px flex-1 bg-neutral-700" />
      <span className="text-[11px] uppercase tracking-wider text-neutral-500">or</span>
      <div className="h-px flex-1 bg-neutral-700" />
    </div>
  );
}

function CopyIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <rect
        x="9"
        y="9"
        width="11"
        height="11"
        rx="2"
        stroke="currentColor"
        strokeWidth="1.5"
      />
      <path
        d="M5 15V6a2 2 0 012-2h9"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
      />
    </svg>
  );
}

function CheckIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M5 12l4.5 4.5L20 6"
        stroke="#05c168"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ArrowUpIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 19V5M5 12l7-7 7 7"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

function ArrowDownIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M12 5v14M19 12l-7 7-7-7"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  );
}

/* Re-export so the parent can render `<ConnectedAddressBadge />` if needed. */
export function AddressBadge({ address }: { address: string }) {
  return (
    <span className="font-mono text-[13px] text-neutral-100">{truncateAddress(address)}</span>
  );
}
