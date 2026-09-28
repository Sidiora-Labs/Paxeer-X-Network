'use client';

import { useEffect, useRef, useState } from 'react';
import { Loader2 } from 'lucide-react';
import { useWalletKind } from '@/providers/WalletKindProvider';
import { useEmbeddedAvailability, useOptionalEmbeddedWallet } from '@/lib/wallet';
import { EmbeddedSignIn } from '@/components/onboarding/EmbeddedSignIn';
import { FundedTierPicker } from '@/components/onboarding/FundedTierPicker';
import { SvgIcon } from '@/components/ui/SvgIcon';
import Image from "next/image";

type Step =
    | 'welcome'
    | 'embedded-signin'
    | 'embedded-setup'
    | 'funded-signin'
    | 'funded-tier-picker';

interface OnboardingProps {
    /**
     * When set, the onboarding screen jumps straight into a sub-flow on
     * mount. Used by the shell to land users in the right screen without
     * a wasted welcome-screen tap when their kind / auth state already
     * implies a specific destination:
     *
     *   - `'embedded-signin'`    — kind is embedded but no session yet.
     *   - `'embedded-setup'`     — embedded session lives but standard
     *                              wallet hasn't been provisioned.
     *   - `'funded-signin'`      — kind is funded but no session yet.
     *   - `'funded-tier-picker'` — funded session lives but no funded
     *                              account has been provisioned.
     */
    initialStep?:
    | 'embedded-signin'
    | 'embedded-setup'
    | 'funded-signin'
    | 'funded-tier-picker';
}

export function Onboarding({ initialStep }: OnboardingProps = {}) {
    const { kind, setKind, clearKind } = useWalletKind();
    const embedded = useOptionalEmbeddedWallet();
    const embeddedAvailable = useEmbeddedAvailability();
    const [step, setStep] = useState<Step>(initialStep ?? 'welcome');
    const [error, setError] = useState('');

    // ── Resume flow on remount ───────────────────────────────────────
    //
    // Onboarding can mount in three flavours depending on prior state:
    //   1. Fresh install — kind=null, step='welcome'.
    //   2. Embedded user signed in but wallet not yet provisioned —
    //      kind='embedded' and isAuthenticated && !publicWallet. We need
    //      to land on the embedded-setup step which auto-provisions.
    //   3. Funded user signed in but no funded account yet —
    //      kind='funded' and isAuthenticated && !fundedSelf. We need to
    //      land on the tier picker.
    useEffect(() => {
        if (!embedded) return;
        if (step !== 'welcome') return;
        if (kind === 'embedded' && embedded.isAuthenticated && !embedded.publicWallet) {
            setStep('embedded-setup');
            return;
        }
        if (kind === 'funded' && embedded.isAuthenticated && !embedded.fundedSelf) {
            setStep('funded-tier-picker');
        }
    }, [
        embedded,
        kind,
        step,
        embedded?.isAuthenticated,
        embedded?.publicWallet,
        embedded?.fundedSelf,
    ]);

    // ── Embedded provision ────────────────────────────────────────────
    // The embedded-setup step kicks the explicit provision once and waits
    // for the provider's `publicWallet` to flip non-null (which routes
    // out of onboarding via the WalletShell `hasWallet` gate).
    const embeddedProvisionTriggeredRef = useRef(false);
    useEffect(() => {
        if (step !== 'embedded-setup') return;
        if (!embedded) return;
        if (embeddedProvisionTriggeredRef.current) return;
        if (!embedded.isAuthenticated) return;
        if (embedded.publicWallet) return;
        embeddedProvisionTriggeredRef.current = true;
        void embedded.provisionStandard().catch((e) => {
            embeddedProvisionTriggeredRef.current = false;
            setError((e as Error).message || 'Failed to set up wallet');
        });
    }, [step, embedded, embedded?.isAuthenticated, embedded?.publicWallet]);

    // ── Welcome ─────────────────────────────────────────────────────────
    if (step === 'welcome') {
        return (
            <div className="min-h-screen flex flex-col items-center justify-center px-6 py-10">
                <div className="flex flex-col items-center gap-6 max-w-sm w-full">
                    {/* Logo */}
                    <div className="w-20 h-20 rounded-3xl bg-pax-accent/10 flex items-center justify-center">
                        <Image
                            src="/paxport_wallet.png"
                            alt="shield"
                            width={48}
                            height={48}
                            className="w-12 h-12"
                        />
                    </div>

                    <div className="text-center">
                        <h1 className="text-2xl font-bold tracking-tight">Paxeer Wallet</h1>
                        <p className="text-sm text-pax-muted mt-2">
                            Pick how you want to manage your wallet on Paxeer Network.
                        </p>
                    </div>

                    <div className="w-full flex flex-col gap-3 mt-2">
                        {/* Paxeer Wallet (embedded). Always rendered so users know the
                option exists; disables with a clear hint when the Supabase
                env vars aren't baked into the client bundle. */}
                        <button
                            onClick={() => {
                                if (!embeddedAvailable) return;
                                setKind('embedded');
                                setStep('embedded-signin');
                            }}
                            disabled={!embeddedAvailable}
                            className={
                                'w-full text-left bg-pax-surface rounded-2xl px-4 py-3.5 press-scale transition-all  ' +
                                (embeddedAvailable
                                    ? 'hover:bg-white/[0.07] '
                                    : 'opacity-60 cursor-not-allowed ')
                            }
                        >
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-xl bg-pax-accent/10 flex items-center justify-center shrink-0 overflow-hidden">
                                    <Image
                                        src="/paxport_wallet.png"
                                        alt=""
                                        width={32}
                                        height={32}
                                        className="w-8 h-8 object-contain"
                                    />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="text-sm font-bold">Paxeer Wallet</span>
                                        {embeddedAvailable ? (
                                            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-pax-accent/15 text-pax-accent">
                                                Recommended
                                            </span>
                                        ) : (
                                            <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-white/[0.06] text-pax-muted">
                                                Unavailable
                                            </span>
                                        )}
                                    </div>
                                    <p className="text-[11px] text-pax-muted mt-0.5 leading-snug">
                                        {embeddedAvailable
                                            ? 'Sign in with email or social. Same wallet on every Paxeer app — no seed phrase.'
                                            : 'Supabase env vars are not set on this build. Add NEXT_PUBLIC_SUPABASE_URL + NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY and rebuild to enable.'}
                                    </p>
                                </div>
                                {embeddedAvailable && (
                                    <SvgIcon
                                        name="chevron-right"
                                        className="w-4 h-4 shrink-0"
                                        style={{ filter: 'brightness(0) invert(0.4)' }}
                                    />
                                )}
                            </div>
                        </button>

                        {/* Funded Account. Uses the same Supabase auth surface as
                            embedded, but signs through the funded policy engine
                            which gates every tx against a tier whitelist. UI hides
                            Send / Receive / Buy when in this mode. */}
                        <button
                            onClick={() => {
                                if (!embeddedAvailable) return;
                                setKind('funded');
                                setStep('funded-signin');
                            }}
                            disabled={!embeddedAvailable}
                            className={
                                'w-full text-left bg-pax-surface rounded-2xl px-4 py-3.5 press-scale transition-all  ' +
                                (embeddedAvailable
                                    ? 'hover:bg-white/[0.07] '
                                    : 'opacity-60 cursor-not-allowed ')
                            }
                        >
                            <div className="flex items-center gap-3">
                                <div className="w-10 h-10 rounded-xl bg-white/[0.06] flex items-center justify-center shrink-0">
                                    <SvgIcon
                                        name="bridge"
                                        className="w-5 h-5"
                                        style={{
                                            filter:
                                                'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)',
                                        }}
                                    />
                                </div>
                                <div className="flex-1 min-w-0">
                                    <div className="flex items-center gap-2 flex-wrap">
                                        <span className="text-sm font-bold">Funded Account</span>
                                        <span className="text-[10px] uppercase tracking-wider px-1.5 py-0.5 rounded-md bg-white/[0.06] text-pax-muted">
                                            Trade with capital
                                        </span>
                                    </div>
                                    <p className="text-[11px] text-pax-muted mt-0.5 leading-snug">
                                        Trade with funded capital on whitelisted apps. No deposit, profit share applies.
                                    </p>
                                </div>
                                {embeddedAvailable && (
                                    <SvgIcon
                                        name="chevron-right"
                                        className="w-4 h-4 shrink-0"
                                        style={{ filter: 'brightness(0) invert(0.4)' }}
                                    />
                                )}
                            </div>
                        </button>
                    </div>

                    {error && <p className="text-red-400 text-xs text-center">{error}</p>}
                </div>
            </div>
        );
    }

    // ── Embedded sign-in ────────────────────────────────────────────────
    if (step === 'embedded-signin') {
        return (
            <EmbeddedSignIn
                onBack={() => {
                    // Roll back the kind selection so the shell renders
                    // the welcome screen again. Just resetting `step`
                    // wouldn't be enough — ShellWidget gates on `kind`
                    // and would route us right back into this screen.
                    clearKind();
                    setStep('welcome');
                }}
            />
        );
    }

    // ── Funded sign-in (reuses EmbeddedSignIn — same Supabase auth) ────
    if (step === 'funded-signin') {
        return (
            <EmbeddedSignIn
                onBack={() => {
                    clearKind();
                    setStep('welcome');
                }}
            />
        );
    }

    // ── Embedded auto-provision step ───────────────────────────────────
    // The `embeddedProvisionTriggeredRef` effect above kicks the explicit
    // provision once we land here; we just render a deterministic loading
    // state until the provider's `publicWallet` flips non-null (which
    // routes out of onboarding through the WalletShell `hasWallet` gate).
    if (step === 'embedded-setup') {
        return (
            <div className="min-h-screen flex items-center justify-center px-6 py-10">
                <div className="flex flex-col items-center gap-4 max-w-sm w-full text-center">
                    <Loader2 className="w-10 h-10 animate-spin text-pax-accent" />
                    <div>
                        <h1 className="text-lg font-bold">Setting up your wallet</h1>
                        <p className="text-sm text-pax-muted mt-1">
                            Generating an EVM account and binding it to your sign-in.
                        </p>
                    </div>
                    {error && <p className="text-red-400 text-xs">{error}</p>}
                </div>
            </div>
        );
    }

    return null;
}
