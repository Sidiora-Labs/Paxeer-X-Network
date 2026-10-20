/**
 * ConnectWalletModal — sign-in surface for HyperPax.
 *
 * Two integrations side-by-side:
 *
 *   1. Paxeer Embedded Wallet (default, recommended)
 *      Email magic link + Google / X / GitHub / Apple / Discord OAuth.
 *      Backed by Supabase auth and the connect.paxportwallet.com signing API.
 *      Auto-provisions a chain-125 EOA on first sign-in. Same wallet across
 *      every Paxeer app the user signs into.
 *
 *   2. Injected wallet (Paxport extension, MetaMask, Rabby, …)
 *      Discovered via wagmi's EIP-6963 multi-injected provider list.
 *      Existing power-user path stays first-class.
 *
 * The modal reads `connectModalOpen` from the Zustand wallet store so any
 * call site (Header, MobileTradeHeader, programmatic) can open it via
 * `useWalletStore.getState().openConnectModal()`.
 *
 * After embedded sign-in completes (Supabase session lands, then publicWallet
 * is provisioned), we automatically `wagmi.connect({ paxeerEmbeddedConnector })`
 * so the rest of the app sees a connected account.
 */
"use client";

import { useEffect, useMemo, useState, type FormEvent } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import { useConnect, useAccount, type Connector } from "wagmi";
import { Mail, Loader2, X as XIcon, Wallet as WalletIcon, ChevronRight } from "lucide-react";
import { useWalletStore } from "@/hooks/wallet/useWalletStore";
import {
  usePaxeer,
  useEmbeddedWalletAvailable,
  PAXEER_CONNECTOR_ID,
  type SignInProvider,
} from "@/core/wallet/embedded";
import { cn } from "@/ui/utils/cn";

type EmailState =
  | { kind: "idle" }
  | { kind: "sending" }
  | { kind: "sent"; email: string }
  | { kind: "error"; message: string };

export function ConnectWalletModal() {
  const open = useWalletStore((s) => s.connectModalOpen);
  const closeModal = useWalletStore((s) => s.closeConnectModal);

  return (
    <Dialog.Root open={open} onOpenChange={(o) => (o ? null : closeModal())}>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-40 bg-black/70 backdrop-blur-sm data-[state=open]:animate-in data-[state=open]:fade-in" />
        <Dialog.Content
          aria-describedby={undefined}
          className={cn(
            "fixed left-1/2 top-1/2 z-50 w-[92vw] max-w-[440px]",
            "-translate-x-1/2 -translate-y-1/2 overflow-hidden rounded-2xl",
            "border border-border bg-surface-1",
            "shadow-[0_24px_48px_rgba(0,0,0,0.4)] focus:outline-none",
            "data-[state=open]:animate-in data-[state=open]:zoom-in-95",
          )}
        >
          <ConnectModalBody onDone={closeModal} />
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

// ─── Body ─────────────────────────────────────────────────────────────────────

function ConnectModalBody({ onDone }: { onDone: () => void }) {
  const embeddedAvailable = useEmbeddedWalletAvailable();
  const paxeer = usePaxeer();
  const { isConnected: wagmiConnected } = useAccount();
  const { connect: wagmiConnect, connectors, isPending: connectPending } = useConnect();
  const [busyConnector, setBusyConnector] = useState<string | null>(null);
  const [postSignInBusy, setPostSignInBusy] = useState(false);

  const injectedConnectors = useMemo(
    () => connectors.filter((c) => c.id !== PAXEER_CONNECTOR_ID),
    [connectors],
  );
  const paxeerConnector = useMemo(
    () => connectors.find((c) => c.id === PAXEER_CONNECTOR_ID),
    [connectors],
  );

  // Once the embedded wallet finishes provisioning, auto-connect wagmi to it
  // and close the modal — the user's already proven who they are.
  useEffect(() => {
    if (!paxeerConnector) return;
    if (!paxeer.isAuthenticated) return;
    if (wagmiConnected) {
      onDone();
      return;
    }
    setPostSignInBusy(true);
    wagmiConnect(
      { connector: paxeerConnector },
      {
        onSuccess: () => {
          setPostSignInBusy(false);
          onDone();
        },
        onError: () => setPostSignInBusy(false),
      },
    );
  }, [paxeer.isAuthenticated, paxeerConnector, wagmiConnected, wagmiConnect, onDone]);

  return (
    <>
      <header className="flex items-center justify-between border-b border-border px-5 py-4">
        <Dialog.Title className="text-[15px] font-semibold text-foreground">
          Connect to HyperPax
        </Dialog.Title>
        <Dialog.Close
          aria-label="Close"
          className="-mr-2 flex h-8 w-8 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-muted/30 hover:text-foreground"
        >
          <XIcon size={16} />
        </Dialog.Close>
      </header>

      <div className="px-5 py-5 space-y-5">
        {embeddedAvailable && (
          <EmbeddedSignInSection
            paxeer={paxeer}
            disabled={postSignInBusy || connectPending}
          />
        )}

        {embeddedAvailable && injectedConnectors.length > 0 && <Divider label="Or" />}

        {injectedConnectors.length > 0 && (
          <InjectedWalletList
            connectors={injectedConnectors}
            disabledAll={postSignInBusy || connectPending || paxeer.authBusy}
            busyConnector={busyConnector}
            onConnect={(c) => {
              setBusyConnector(c.id);
              wagmiConnect(
                { connector: c },
                {
                  onSettled: () => setBusyConnector(null),
                  onSuccess: () => onDone(),
                },
              );
            }}
          />
        )}

        {postSignInBusy && (
          <p className="flex items-center justify-center gap-2 text-[12px] text-muted-foreground">
            <Loader2 className="h-3.5 w-3.5 animate-spin" />
            Provisioning your Paxeer wallet…
          </p>
        )}

        <p className="text-center text-[11px] text-muted-foreground/80">
          By continuing you agree to Paxeer&apos;s terms of service.
        </p>
      </div>
    </>
  );
}

// ─── Embedded sign-in (Supabase auth) ─────────────────────────────────────────

function EmbeddedSignInSection({
  paxeer,
  disabled,
}: {
  paxeer: ReturnType<typeof usePaxeer>;
  disabled: boolean;
}) {
  const [email, setEmail] = useState("");
  const [emailState, setEmailState] = useState<EmailState>({ kind: "idle" });
  const [oauthLoading, setOauthLoading] = useState<SignInProvider | null>(null);

  async function clickProvider(provider: SignInProvider) {
    if (disabled) return;
    setOauthLoading(provider);
    try {
      sessionStorage.setItem(
        "paxeer:auth:returnTo",
        window.location.pathname + window.location.search,
      );
    } catch { /* quota / privacy */ }
    try {
      await paxeer.signInWithOAuth(provider);
      // Browser redirects away — never returns here on success.
    } catch {
      setOauthLoading(null);
    }
  }

  async function submitEmail(e: FormEvent) {
    e.preventDefault();
    if (!email.includes("@")) return;
    try {
      sessionStorage.setItem(
        "paxeer:auth:returnTo",
        window.location.pathname + window.location.search,
      );
    } catch { /* quota / privacy */ }
    setEmailState({ kind: "sending" });
    const r = await paxeer.signInWithEmail(email);
    setEmailState(
      r.ok
        ? { kind: "sent", email }
        : { kind: "error", message: r.error ?? "Sign-in failed" },
    );
  }

  if (emailState.kind === "sent") {
    return (
      <div className="flex flex-col items-center gap-3 rounded-xl border border-border bg-surface-2 px-5 py-7 text-center">
        <div className="flex h-10 w-10 items-center justify-center rounded-full bg-blue-500/10 text-blue-400">
          <Mail size={20} />
        </div>
        <h3 className="text-[15px] font-medium text-foreground">Check your email</h3>
        <p className="text-[13px] text-muted-foreground">
          We sent a magic link to <span className="text-foreground">{emailState.email}</span>.
          <br />
          Click it to finish signing in.
        </p>
        <button
          type="button"
          onClick={() => setEmailState({ kind: "idle" })}
          className="mt-1 text-[12px] text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
        >
          Use a different email
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <div className="space-y-2">
        <h3 className="text-[12px] font-medium uppercase tracking-wider text-muted-foreground">
          Paxeer Wallet
        </h3>
        <p className="text-[12px] leading-snug text-muted-foreground">
          No seed phrase, no extension. One sign-in, one wallet across every Paxeer app.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <ProviderTile
          label="Google"
          icon="/icons/wallet/google.svg"
          onClick={() => clickProvider("google")}
          loading={oauthLoading === "google"}
          disabled={disabled || oauthLoading !== null || paxeer.authBusy}
        />
        <ProviderTile
          label="X"
          icon="/icons/wallet/x.svg"
          onClick={() => clickProvider("twitter")}
          loading={oauthLoading === "twitter"}
          disabled={disabled || oauthLoading !== null || paxeer.authBusy}
        />
        <ProviderTile
          label="GitHub"
          icon="/icons/wallet/github.svg"
          onClick={() => clickProvider("github")}
          loading={oauthLoading === "github"}
          disabled={disabled || oauthLoading !== null || paxeer.authBusy}
        />
        <ProviderTile
          label="Email"
          icon="/icons/wallet/email.svg"
          onClick={() => {
            const el = document.getElementById("paxeer-email-input") as HTMLInputElement | null;
            el?.focus();
          }}
          disabled={disabled || oauthLoading !== null || paxeer.authBusy}
        />
      </div>

      <form onSubmit={submitEmail} className="flex flex-col gap-2">
        <input
          id="paxeer-email-input"
          type="email"
          autoComplete="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          disabled={emailState.kind === "sending" || disabled || paxeer.authBusy}
          className={cn(
            "w-full rounded-xl border border-border bg-background",
            "px-3.5 py-2.5 text-[13px] text-foreground",
            "placeholder:text-muted-foreground/60",
            "focus:border-blue-500 focus:outline-none focus:ring-0",
            "disabled:cursor-not-allowed disabled:opacity-50",
          )}
        />
        <button
          type="submit"
          disabled={
            !email.includes("@") || emailState.kind === "sending" || disabled || paxeer.authBusy
          }
          className={cn(
            "flex h-11 items-center justify-center gap-2 rounded-xl",
            "bg-blue-600 text-[13px] font-semibold text-white",
            "transition-colors hover:bg-blue-500",
            "disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-blue-600",
          )}
        >
          {emailState.kind === "sending" ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" /> Sending magic link…
            </>
          ) : (
            <>Continue with email</>
          )}
        </button>
        {emailState.kind === "error" && (
          <p className="text-[11px] text-red-400">{emailState.message}</p>
        )}
        {paxeer.authError && emailState.kind !== "error" && (
          <p className="text-[11px] text-red-400">{paxeer.authError}</p>
        )}
      </form>
    </div>
  );
}

function ProviderTile({
  label,
  icon,
  onClick,
  loading,
  disabled,
}: {
  label: string;
  icon: string;
  onClick: () => void;
  loading?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      className={cn(
        "flex h-11 items-center justify-center gap-2 rounded-xl",
        "border border-border bg-surface-2 text-[13px] font-medium text-foreground",
        "transition-colors hover:border-muted hover:bg-muted/30",
        "disabled:cursor-not-allowed disabled:opacity-50",
      )}
    >
      {loading ? (
        <Loader2 className="h-4 w-4 animate-spin" />
      ) : (
        // eslint-disable-next-line @next/next/no-img-element
        <img src={icon} alt="" aria-hidden="true" className="h-4 w-4" />
      )}
      <span>{label}</span>
    </button>
  );
}

// ─── Injected wallets ─────────────────────────────────────────────────────────

interface InjectedWalletListProps {
  connectors: readonly Connector[];
  busyConnector: string | null;
  disabledAll: boolean;
  onConnect: (c: Connector) => void;
}

function InjectedWalletList({
  connectors,
  busyConnector,
  disabledAll,
  onConnect,
}: InjectedWalletListProps) {
  return (
    <div className="space-y-3">
      <h3 className="text-[12px] font-medium uppercase tracking-wider text-muted-foreground">
        Browser wallets
      </h3>
      <div className="flex flex-col gap-1.5">
        {connectors.map((c) => {
          const isBusy = busyConnector === c.id;
          return (
            <button
              key={c.id}
              type="button"
              onClick={() => onConnect(c)}
              disabled={disabledAll || busyConnector !== null}
              className={cn(
                "flex h-12 items-center justify-between gap-3 rounded-xl",
                "border border-border bg-surface-2 px-3.5",
                "text-left text-[13px] font-medium text-foreground",
                "transition-colors hover:border-muted hover:bg-muted/30",
                "disabled:cursor-not-allowed disabled:opacity-50",
              )}
            >
              <span className="flex items-center gap-3">
                {c.icon ? (
                  // eslint-disable-next-line @next/next/no-img-element
                  <img src={c.icon} alt="" aria-hidden="true" className="h-5 w-5 rounded" />
                ) : (
                  <span className="flex h-5 w-5 items-center justify-center rounded bg-muted/40 text-muted-foreground">
                    <WalletIcon size={12} />
                  </span>
                )}
                <span>{c.name}</span>
              </span>
              {isBusy ? (
                <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
              ) : (
                <ChevronRight size={16} className="text-muted-foreground" />
              )}
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ─── Misc ────────────────────────────────────────────────────────────────────

function Divider({ label }: { label: string }) {
  return (
    <div className="flex items-center gap-3">
      <div className="h-px flex-1 bg-border" />
      <span className="text-[10px] uppercase tracking-wider text-muted-foreground">{label}</span>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}
